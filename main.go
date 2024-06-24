package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"sync"
	"syscall"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/tags"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/json"
)

var authToken = os.Getenv("MINIO_WEBHOOK_AUTH_TOKEN")
var port = os.Getenv("MINIO_WEBHOOK_PORT")
var claimAVendpoint = os.Getenv("MINIO_CLAMAV_ENDPOINT")
var claimAVaccessKeyID = os.Getenv("MINIO_CLAMAV_ACCESS_KEY")
var claimAVsecretAccessKey = os.Getenv("MINIO_CLAMAV_SECRET")
var v5AuthHeaderRegexp = regexp.MustCompile(`AWS4-HMAC-SHA256 Credential=(?P<AccessKeyId>[\w-]+)/(?P<Date>\d{8})/(?P<Region>[\w\-]+)/(?P<Service>[\w\-]+)/aws4_request,\s*SignedHeaders=(?P<SignatureHeaders>[\w\-\;]+),\s*Signature=(?P<Signature>[a-f0-9]{64})`)

// LogEntry represents a Minio log entry
type LogEntry struct {
	Version        string            `json:"version"`
	DeploymentID   string            `json:"deploymentid"`
	Event          string            `json:"event"`
	Trigger        string            `json:"trigger"`
	Time           metav1.Time       `json:"time"`
	API            API               `json:"api"`
	RemoteHost     string            `json:"remotehost"`
	RequestID      string            `json:"requestID"`
	UserAgent      string            `json:"userAgent"`
	RequestHeader  map[string]string `json:"requestHeader"`
	ResponseHeader map[string]string `json:"responseHeader"`
	Tags           Tags              `json:"tags"`
	authInfo       map[string]string
}

// API represents the details of an API call
type API struct {
	Name            string          `json:"name"`
	Bucket          string          `json:"bucket"`
	Object          string          `json:"object"`
	Status          string          `json:"status"`
	StatusCode      int             `json:"statusCode"`
	RX              int             `json:"rx"`
	TX              int             `json:"tx"`
	TimeToResponse  metav1.Duration `json:"timeToResponse"`
	TimeToFirstByte metav1.Duration `json:"timeToFirstByte,omitempty"`
}

// Tags contain extra details on how a request was served
type Tags struct {
	ObjectErasureMap map[string]Object `json:"objectErasureMap,omitempty"`
}

// Object contains details on where an object was retrieved from
type Object struct {
	PoolID int      `json:"poolId"`
	SetID  int      `json:"setId"`
	Disks  []string `json:"disks"`
}

// AccessKeyID returns the AccessKeyID used to make the request, if it was authenticated
func (l *LogEntry) AccessKeyID() string {
	authInfo := l.getAuthInfo()
	if a, ok := authInfo["AccessKeyId"]; ok {
		return a
	}
	return "-"
}

func (l *LogEntry) getAuthInfo() map[string]string {
	if l.authInfo == nil {
		l.authInfo = make(map[string]string)
		if headerValue := l.RequestHeader["Authorization"]; headerValue != "" {
			match := v5AuthHeaderRegexp.FindStringSubmatch(headerValue)
			if len(match) > 1 {
				for i, name := range v5AuthHeaderRegexp.SubexpNames() {
					if i > 0 && name != "" {
						l.authInfo[name] = match[i]
					}
				}
			}
		}
	}
	return l.authInfo
}

func scanFile(bucket, object string) {
	useSSL := true

	// Initialize MinIO client
	customTransport := http.DefaultTransport.(*http.Transport).Clone()
	customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	minioClient, err := minio.New(claimAVendpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(claimAVaccessKeyID, claimAVsecretAccessKey, ""),
		Secure:    useSSL,
		Transport: customTransport,
	})
	if err != nil {
		log.Printf("Initialize MinIO client failed: %s", err)
	}

	// Download object to temp file
	tempFile, err := os.CreateTemp("/tmp", "")
	if err != nil {
		log.Printf("os.CreateTemp failed: %s", err)
	}
	defer os.Remove(tempFile.Name())

	reader, err := minioClient.GetObject(context.Background(), bucket, object, minio.GetObjectOptions{})
	if err != nil {
		log.Printf("minioClient.GetObject failed: %s", err)
	}
	defer reader.Close()

	stat, err := reader.Stat()
	if err != nil {
		log.Printf("reader.Stat() failed: %s", err)
	}

	if _, err := io.CopyN(tempFile, reader, stat.Size); err != nil {
		log.Printf("io.CopyN failed: %s", err)
	}

	// Scan with ClaimAV
	cmd := exec.Command("clamdscan", "--no-summary", "--remove", tempFile.Name())
	if err := cmd.Run(); err != nil {
		log.Printf("exec.Command failed: %s", err)
	}

	var tagMap = map[string]string{}
	if _, err := os.Stat(tempFile.Name()); err == nil {
		tagMap = map[string]string{
			"ClamAV": "clean",
		}
	} else {
		tagMap = map[string]string{
			"ClamAV": "infected",
		}
		log.Printf("WARNING: Infected object found. Bucket: %s Object: %s", bucket, object)
	}
	t, err := tags.MapToObjectTags(tagMap)
	if err != nil {
		log.Printf("tags.MapToObjectTags failed: %s", err)
	}
	err = minioClient.PutObjectTagging(context.Background(), bucket, object, t, minio.PutObjectTaggingOptions{})
	if err != nil {
		log.Printf("minioClient.PutObjectTagging failed: %s", err)
	}
}

func main() {
	var logFile io.WriteCloser
	var logFileMu sync.Mutex
	var err error

	if len(os.Args) == 3 {
		logFile, err = os.OpenFile(os.Args[2], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
		if err != nil {
			log.Fatal(err)
		}
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGHUP)

		go func() {
			for range sigs {
				logFileMu.Lock()
				logFile.Close()
				logFile, err = os.OpenFile(os.Args[2], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
				if err != nil {
					log.Fatal(err)
				}
				logFileMu.Unlock()
			}
		}()
	} else {
		logFile = os.Stdout
	}
	if port == "" {
		port = "8080"
	}

	connString := os.Getenv("MINIO_MSSQL_CONNECTION_STRING")
	var conn *sql.DB
	if connString != "" {
		conn, err = sql.Open("mssql", connString)
		if err != nil {
			log.Fatal("Open connection to SQL server failed: ", err.Error())
		}
		defer conn.Close()

		err = conn.Ping()
		if err != nil {
			log.Fatal("Cannot connect to SQL server: ", err.Error())
		}
	}

	log.Printf("Listening on port %s", port)
	err = http.ListenAndServe(":"+port, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authToken != "" {
			if authToken != r.Header.Get("Authorization") {
				return
			}
		}
		switch r.Method {
		case "POST":
			entry := &LogEntry{}
			if os.Getenv("MINIO_WEBHOOK_FORMAT") == "raw" {
				data, err := io.ReadAll(r.Body)
				if err != nil {
					log.Printf("Failed to read log entry: %v", err)
					return
				}
				logFileMu.Lock()
				fmt.Fprintf(logFile, "%s\n", string(data))
				logFileMu.Unlock()
			} else {
				decoder := json.NewDecoderCaseSensitivePreserveInts(r.Body)
				if err := decoder.Decode(entry); err != nil {
					log.Printf("Failed to decode log entry: %v", err)
					return
				}

				// Do not log MinIO internal operations
				if entry.API.Bucket == "" || entry.API.Bucket == ".minio.sys" {
					return
				}
				// Do not log non-authentication operations (nmap scanning, etc)
				if entry.AccessKeyID() == "-" {
					return
				}
				// Do not log ClamAV tagging done by this tool itselves
				if entry.AccessKeyID() == claimAVaccessKeyID {
					return
				}

				if os.Getenv("MINIO_WEBHOOK_SKIP_BROWSING") == "true" {
					if IsBrowsingEvent(entry.API.Name) {
						return
					}
				}

				if os.Getenv("MINIO_MSSQL_CONNECTION_STRING") != "" {
					if entry.API.Name == "PutObject" || entry.API.Name == "CompleteMultipartUpload" {
						_, err = conn.Exec("SaveObject @Bucket=?,@Path=?,@AccessKeyID=?,@RemoteHost=?;", entry.API.Bucket, entry.API.Object, entry.AccessKeyID(), entry.RemoteHost)
						if err != nil {
							log.Printf("Stored procedure SaveObject failed: %s", err)
						}
						go scanFile(entry.API.Bucket, entry.API.Object)
					}
					if entry.API.Name == "DeleteObject" {
						_, err = conn.Exec("DeleteObject @Bucket=?,@Path=?", entry.API.Bucket, entry.API.Object)
						if err != nil {
							log.Printf("Stored procedure DeleteObject failed: %s", err)
						}
					}
				}

				logFileMu.Lock()
				fmt.Fprintf(logFile, "%s [%s] %s %s %s %s %s %d %d %d %d %q %q %s %s\n",
					entry.API.Bucket, entry.Time.Format("02/Jan/2006:15:04:05 -0700"), entry.RemoteHost, entry.AccessKeyID(), entry.RequestID, entry.API.Name, entry.API.Object,
					entry.API.StatusCode, entry.API.TX, entry.API.TimeToResponse.Milliseconds(), entry.API.TimeToFirstByte.Milliseconds(),
					entry.RequestHeader["Referer"], entry.UserAgent, entry.DeploymentID, entry.RequestHeader["X-Forwarded-Host"])
				logFileMu.Unlock()
			}
		default:
		}
	}))
	if err != nil {
		log.Fatal(err)
	}
}

func IsBrowsingEvent(api string) bool {
	switch api {
	case
		"AccountInfo",
		"AssumeRole",
		"GetBucketEncryption",
		"GetBucketLifecycle",
		"GetBucketLocation",
		"GetBucketObjectLockConfig",
		"GetBucketPolicy",
		"GetBucketQuotaConfig",
		"GetBucketReplicationConfig",
		"GetBucketTagging",
		"GetBucketVersioning",
		"GetConfigKV",
		"GetGroup",
		"GetIdentityProviderCfg",
		"GetObjectLegalHold",
		"GetObjectRetention",
		"GetObjectTagging",
		"HeadBucket",
		"HeadObject",
		"HelpConfigKV",
		"InfoCannedPolicy",
		"KMSAPIs",
		"KMSListKeys",
		"KMSMetrics",
		"KMSStatus",
		"KMSVersion",
		"ListBuckets",
		"ListCannedPolicies",
		"ListGroups",
		"ListIdentityProviderCfg",
		"ListObjectsV2",
		"ListObjectVersions",
		"ListServiceAccounts",
		"ListTier",
		"ListUsers",
		"ServerInfo",
		"SiteReplicationInfo",
		"TierStats":
		return true
	}
	return false
}
