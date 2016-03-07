package main

import (
	"bufio"
	//"fmt"
	"github.com/fsouza/go-dockerclient"
	"github.com/benchflow/commons/minio"
	"github.com/Shopify/sarama"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"bytes"
	"strings"
	"strconv"
)

type Container struct {
	ID string
}

var containers [10]Container
var client docker.Client

type KafkaMessage struct {
	SUT_name string `json:"SUT_name"`
	SUT_version string `json:"SUT_version"`
	Minio_key string `json:"minio_key"`
	Trial_id string `json:"trial_id"`
	Experiment_id string `json:"experiment_id"`
	Total_trials_num int `json:"total_trials_num"`
	Collector_name string `json:"collector_name"`
	}

func signalOnKafka(minioKey string) {
	totalTrials, _ := strconv.Atoi(os.Getenv("TOTAL_TRIALS_NUM"))
	kafkaMsg := KafkaMessage{SUT_name: os.Getenv("SUT_NAME"), SUT_version: os.Getenv("SUT_VERSION"), Minio_key: minioKey, Trial_id: os.Getenv("TRIAL_ID"), Experiment_id: os.Getenv("EXPERIMENT_ID"), Total_trials_num: totalTrials, Collector_name: os.Getenv("COLLECTOR_NAME")}
	jsMessage, err := json.Marshal(kafkaMsg)
	if err != nil {
		log.Printf("Failed to marshall json message")
		}
	producer, err := sarama.NewSyncProducer([]string{os.Getenv("KAFKA_HOST")+":9092"}, nil)
	if err != nil {
	    log.Fatalln(err)
	}
	defer func() {
	    if err := producer.Close(); err != nil {
	        log.Fatalln(err)
	    }
	}()
	msg := &sarama.ProducerMessage{Topic: os.Getenv("KAFKA_TOPIC"), Value: sarama.StringEncoder(jsMessage)}
	partition, offset, err := producer.SendMessage(msg)
	if err != nil {
	    log.Printf("FAILED to send message: %s\n", err)
	    } else {
	    log.Printf("> message sent to partition %d at offset %d\n", partition, offset)
	    }
	}

func collectStats(container Container, since int64) {
	fo, _ := os.Create(container.ID + "_tmp")
	writerOut := bufio.NewWriter(fo)
	fe, _ := os.Create(container.ID + "_tmp_err")
	writerErr := bufio.NewWriter(fe)
	err := client.Logs(docker.LogsOptions{
		Container:    container.ID,
		OutputStream: writerOut,
		ErrorStream:  writerErr,
		Follow:       false,
		Stdout:       true,
		Stderr:       true,
		Since:        since,
		Timestamps:   true,
	})
	if err != nil {
		log.Fatal(err)
	}
	
	fo.Close()
	fe.Close()
	
	minio.GzipFile(container.ID+"_tmp")
	minio.GzipFile(container.ID+"_tmp_err")
	
	minioKey := minio.GenerateKey("logs.gz")
	callMinioClient(container.ID+"_tmp.gz", os.Getenv("MINIO_ALIAS"), minioKey)
	//minio.StoreOnMinio(container.ID+"_tmp.gz", "runs", minioKey)
	signalOnKafka(minioKey)
	
	minioKey = minio.GenerateKey("logs_err.gz")
	callMinioClient(container.ID+"_tmp_err.gz", os.Getenv("MINIO_ALIAS"), minioKey)
	//minio.StoreOnMinio(container.ID+"_tmp_err.gz", "runs", minioKey)
	signalOnKafka(minioKey)
}

func storeData(w http.ResponseWriter, r *http.Request) {
	contEV := os.Getenv("CONTAINERS")
	conts := strings.Split(contEV, ",")
	since := r.FormValue("since")
	sinceInt, err := strconv.ParseInt(since, 10, 64)
	if err != nil {
		panic(err)
		}
	for i, _ := range conts {
		collectStats(containers[i], sinceInt)
	}
}

func createDockerClient() docker.Client {
	//path := os.Getenv("DOCKER_CERT_PATH")
	//endpoint := "tcp://"+os.Getenv("DOCKER_HOST")+":2376"
	//endpoint = "tcp://192.168.99.100:2376"
    //ca := fmt.Sprintf("%s/ca.pem", path)
    //cert := fmt.Sprintf("%s/cert.pem", path)
    //key := fmt.Sprintf("%s/key.pem", path)
    //client, err := docker.NewTLSClient(endpoint, cert, key, ca)
	endpoint := "unix:///var/run/docker.sock"
    client, err := docker.NewClient(endpoint)
	if err != nil {
		log.Fatal(err)
		}
	return *client
	}

func callMinioClient(fileName string, minioHost string, minioKey string) {
		//TODO: change, we are using sudo to elevate the priviledge in the container, but it is not nice
		//NOTE: it seems that the commands that are not in PATH, should be launched using sh -c
		log.Printf("sh -c sudo /app/mc --quiet cp " + fileName + " " + minioHost + "/runs/" + minioKey)
		cmd := exec.Command("sh", "-c", "sudo /app/mc --quiet cp " + fileName + " " + minioHost + "/runs/" + minioKey)
    	var out bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err != nil {
			log.Println(err)
		    log.Println(stderr.String())
		    return
		}
		log.Println("Result: " + out.String())
	}

func main() {
	client = createDockerClient()
	contEV := os.Getenv("CONTAINERS")
	conts := strings.Split(contEV, ",")
	for i, each := range conts {
		c := Container{ID: each}
		containers[i] = c
	}
	http.HandleFunc("/store", storeData)
	http.ListenAndServe(":8080", nil)
}
