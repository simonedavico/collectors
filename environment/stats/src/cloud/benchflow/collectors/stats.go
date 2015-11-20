package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
	"github.com/fsouza/go-dockerclient"
	"github.com/minio/minio-go"
)

type Container struct {
	ID           string
	statsChannel chan *docker.Stats
	flagsChannel chan bool
	stopChannel chan bool
}

var containers [10]Container
var collecting bool

func collectStats(client docker.Client, container Container) {
	go func() {
		err := client.Stats(docker.StatsOptions{
			ID:      container.ID,
			Stats:   container.statsChannel,
			Stream:  true,
			Done:    container.flagsChannel,
			Timeout: time.Duration(10),
		})
		if err != nil {
			log.Fatal(err)
		}
	}()
}

func monitorStats(container *Container) {
	go func() {
		var e docker.Env
		fo, _ := os.Create(container.ID+"_tmp")
		for true {
			select {
			case _ = <- container.stopChannel:
				//container.flagsChannel <- true
				fo.Close()
				gzipFile(container.ID+"_tmp")
				storeOnMinio(container.ID+"_tmp")
				return
			default:
				dat := (<-container.statsChannel)
				e.SetJSON("dat", dat)
				fo.Write([]byte(e.Get("dat")))
				fo.Write([]byte("\n"))
				}
		}
	}()
}

func gzipFile(fileName string) {
	cmd := exec.Command("gzip", fileName)
	err := cmd.Start()
	cmd.Wait()
	if err != nil {
		panic(err)
		}
	}

func storeOnMinio(fileName string) {
	config := minio.Config{
		AccessKeyID:     os.Getenv("MINIO_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("MINIO_SECRET_ACCESS_KEY"),
		Endpoint:        os.Getenv("MINIO_HOST"),
		}
		s3Client, err := minio.New(config)
	    if err != nil {
	        log.Fatalln(err)
	    }  
	    object, err := os.Open(fileName)
		if err != nil {
			log.Fatalln(err)
		}
		defer object.Close()
		objectInfo, err := object.Stat()
		if err != nil {
			object.Close()
			log.Fatalln(err)
		}
		err = s3Client.PutObject("benchmarks/a/runs/1", os.Getenv("CONTAINER_NAME")+"_stats.gz", "application/octet-stream", objectInfo.Size(), object)
		if err != nil {
			log.Fatalln(err)
		}
	}

func startCollecting(w http.ResponseWriter, r *http.Request) {
	if collecting {
		fmt.Fprintf(w, "Already collecting")
		return
	}
	path := os.Getenv("DOCKER_CERT_PATH")
	endpoint := os.Getenv("DOCKER_HOST")
	endpoint = "tcp://192.168.99.100:2376"
    ca := fmt.Sprintf("%s/ca.pem", path)
    cert := fmt.Sprintf("%s/cert.pem", path)
    key := fmt.Sprintf("%s/key.pem", path)
    client, err := docker.NewTLSClient(endpoint, cert, key, ca)
	//endpoint := "unix:///var/run/docker.sock"
    //client, err := docker.NewClient(endpoint)
	if err != nil {
		log.Fatal(err)
	}
	contEV := os.Getenv("CONTAINERS")
	conts := strings.Split(contEV, ":")
	for i, each := range conts {
		statsChannel := make(chan *docker.Stats)
		flagsChannel := make(chan bool)
		stopChannel := make(chan bool)
		c := Container{ID: each, statsChannel: statsChannel, flagsChannel: flagsChannel, stopChannel : stopChannel}
		containers[i] = c
		collectStats(*client, containers[i])
		monitorStats(&containers[i])
	}
	collecting = true
	fmt.Fprintf(w, "Started collecting")
}

func stopCollecting(w http.ResponseWriter, r *http.Request) {
	if !collecting {
		fmt.Fprintf(w, "Currently not collecting")
		return
	}
	for _, c := range containers {
		c.stopChannel <- true
		}
	collecting = false
	fmt.Fprintf(w, "Stopped collecting")
}

func main() {
	collecting = false
	
	http.HandleFunc("/start", startCollecting)
	http.HandleFunc("/stop", stopCollecting)
	http.ListenAndServe(":8080", nil)
}
