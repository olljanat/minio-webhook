package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/json"
)

// LogEntry represents a Minio log entry
type LogEntry struct {
	Version        string            `json:"version"`
	DeploymentID   string            `json:"deploymentid"`
	Time           metav1.Time       `json:"time"`
	API            API               `json:"api"`
	RemoteHost     string            `json:"remotehost"`
	RequestID      string            `json:"requestID"`
	UserAgent      string            `json:"userAgent"`
	RequestHeader  map[string]string `json:"requestHeader"`
	ResponseHeader map[string]string `json:"responseHeader"`
	Tags           Tags              `json:"tags"`
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
	TimeToFirstByte metav1.Duration `json:"timeToFirstByte"`
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

var authToken = os.Getenv("MINIO_WEBHOOK_AUTH_TOKEN")
var port = os.Getenv("MINIO_WEBHOOK_PORT")

func main() {
	var logFile io.WriteCloser
	var logFileMu sync.Mutex
	var err error

	if len(os.Args) == 2 {
		logFile, err = os.OpenFile(os.Args[1], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
		if err != nil {
			log.Fatal(err)
		}
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGHUP)

		go func() {
			for range sigs {
				logFileMu.Lock()
				logFile.Close()
				logFile, err = os.OpenFile(os.Args[1], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
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
			if os.Getenv("OUTPUT_FORMAT") == "raw" {
				data, err := ioutil.ReadAll(r.Body)
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
				logFileMu.Lock()
				fmt.Fprintf(logFile, "%s %s %s [%s] %s %s/%s %d %d\n", entry.RemoteHost, "-", "-", entry.Time.Time, entry.API.Name, entry.API.Bucket, entry.API.Object, entry.API.StatusCode, entry.API.TX)
				logFileMu.Unlock()
			}
		default:
		}
	}))
	if err != nil {
		log.Fatal(err)
	}
}
