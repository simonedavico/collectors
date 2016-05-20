package main

import (
	"fmt"
	"net"
	"time"
	"log"
	"net/http"
	"os"
	"os/exec"
	"bytes"
	"strings"
	"sync"
	"github.com/fsouza/go-dockerclient"
	"github.com/Shopify/sarama"
	"encoding/json"
	"github.com/benchflow/commons/minio"
	"strconv"
)

type Container struct {
	Name         string
	ID           string
	statsChannel chan *docker.Stats
	doneChannel chan bool
	Network       string
}

var containers []Container
var stopChannel chan bool
//var doneChannel chan bool
var waitGroup sync.WaitGroup
var collecting bool

type KafkaMessage struct {
	SUT_name string `json:"SUT_name"`
	SUT_version string `json:"SUT_version"`
	Minio_key string `json:"minio_key"`
	Trial_id string `json:"trial_id"`
	Experiment_id string `json:"experiment_id"`
	Container_id string `json:"container_id"`
	Total_trials_num int `json:"total_trials_num"`
	Collector_name string `json:"collector_name"`
	}

func signalOnKafka(minioKey string, containerID string) {
	totalTrials, _ := strconv.Atoi(os.Getenv("BENCHFLOW_TRIAL_TOTAL_NUM"))
	kafkaMsg := KafkaMessage{SUT_name: os.Getenv("SUT_NAME"), SUT_version: os.Getenv("SUT_VERSION"), Minio_key: minioKey, Trial_id: os.Getenv("BENCHFLOW_TRIAL_ID"), Experiment_id: os.Getenv("BENCHFLOW_EXPERIMENT_ID"), Container_id: containerID, Total_trials_num: totalTrials, Collector_name: os.Getenv("BENCHFLOW_COLLECTOR_NAME")}
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

func attachToContainer(client docker.Client, container Container) {
	go func() {
		_ = client.Stats(docker.StatsOptions{
			ID:      container.ID,
			Stats:   container.statsChannel,
			Stream:  true,
			Done:    container.doneChannel,
			Timeout: 0,
		})
	}()
}

func collectStats(container Container) {
	go func() {
		var e docker.Env
		fo, err := os.Create("/app/"+container.Name+"_stats_tmp")
		if err != nil {
	        panic(err)
	    }
		for true {
			select {
			case <- stopChannel:
				close(container.doneChannel)
				fo.Close()
				waitGroup.Done()
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

func collectNetworkStats(container Container, client docker.Client) {
	go func() {
		foNet, err := os.Create("/app/"+container.Name+"_network_tmp")
		if err != nil {
	        panic(err)
	    }
		foTop, err := os.Create("/app/"+container.Name+"_top_tmp")
		if err != nil {
	        panic(err)
	    }
		interfaces, err := net.Interfaces()
		if err != nil {
	        panic(err)
	    	}
		var nethogsOptions []string
		nethogsOptions = append(nethogsOptions, "-t")
		var interfaceNames []string
		for _, each := range interfaces {
			interfaceNames = append(interfaceNames, each.Name)
			}
		nethogsOptions = append(nethogsOptions, interfaceNames...)
		cmd := exec.Command("/usr/usr/local/sbin/nethogs", nethogsOptions...)
		cmd.Stdout = foNet
		err = cmd.Start()
	    if err != nil {
	        panic(err)
	    	}
		for true {
			select {
			default:
				top, err := client.TopContainer(container.Name, "")
				if err != nil {
		        	panic(err)
		    	}
				var e docker.Env
				e.SetJSON("top", top)
				foTop.WriteString(e.Get("top")+"\n")
				time.Sleep(750 * time.Millisecond)
			case <- stopChannel:
				cmd.Process.Kill()
				foNet.Close()
				foTop.Close()
				waitGroup.Done()
				return
				}
		}
	}()
}

func createDockerClient() docker.Client {
	//path := os.Getenv("DOCKER_CERT_PATH")
	//endpoint := "tcp://192.168.99.100:2376"
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

func startCollecting(w http.ResponseWriter, r *http.Request) {
	if collecting {
		fmt.Fprintf(w, "Already collecting")
		return
	}
	client := createDockerClient()
	contEV := os.Getenv("CONTAINERS")
	conts := strings.Split(contEV, ",")
	containers = []Container{}
	stopChannel = make(chan bool)
	for _, each := range conts {
		containerInspect, err := client.InspectContainer(each)
		networks := containerInspect.NetworkSettings.Networks
		// Assuming bridge by default if not host
		/*
		Possible values of a network:
		--net="bridge"          Connect a container to a network
                                'bridge': create a network stack on the default Docker bridge
                                'none': no networking
                                'container:<name|id>': reuse another container's network stack
                                'host': use the Docker host network stack
                                '<network-name>|<network-id>': connect to a user-defined network
		*/
		network := "bridge"
		for k := range networks {
			if k == "host" {
				network = "host"
				}
			}
		ID := containerInspect.ID
		if err != nil {
			panic(err)
			}
		statsChannel := make(chan *docker.Stats)
		doneChannel := make(chan bool)
		c := Container{Name: each, ID: ID, statsChannel: statsChannel, doneChannel: doneChannel, Network: network}
		containers = append(containers, c)
		attachToContainer(client, c)
		collectStats(c)
		waitGroup.Add(1)
		if(network == "host") {
			collectNetworkStats(c, client)
			waitGroup.Add(1)
		}
	}
	collecting = true
	fmt.Fprintf(w, "Started collecting")
}

func stopCollecting(w http.ResponseWriter, r *http.Request) {
	if !collecting {
		fmt.Fprintf(w, "Currently not collecting")
		return
	}
	close(stopChannel)
	waitGroup.Wait()
	collecting = false
	composedMinioKey := ""
	composedContainerIds := ""
	for _, container := range containers {
		minio.GzipFile("/app/"+container.Name+"_stats_tmp")
		if container.Network == "host" {
			minio.GzipFile("/app/"+container.Name+"_network_tmp")
			minio.GzipFile("/app/"+container.Name+"_top_tmp")
		}
		minioKey := minio.GenerateKey(container.Name)
		composedMinioKey = composedMinioKey+minioKey+","
		composedContainerIds = composedContainerIds+container.ID+","
		callMinioClient("/app/"+container.ID+"_stats_tmp.gz", os.Getenv("MINIO_ALIAS"), minioKey+"_stats.gz")
		if container.Network == "host" {
			callMinioClient("/app/"+container.ID+"_network_tmp.gz", os.Getenv("MINIO_ALIAS"), minioKey+"_network.gz")
			callMinioClient("/app/"+container.ID+"_top_tmp.gz", os.Getenv("MINIO_ALIAS"), minioKey+"_top.gz")
		}
		err := os.Remove("/app/"+container.ID+"_stats_tmp.gz")
		if err != nil {
	        panic(err)
	    }
		if container.Network == "host" {
			err = os.Remove("/app/"+container.ID+"_network_tmp.gz")
			if err != nil {
		        panic(err)
		    }
			err = os.Remove("/app/"+container.ID+"_top_tmp.gz")
			if err != nil {
		        panic(err)
		    }
		}
	}
	composedMinioKey = strings.TrimRight(composedMinioKey, ",")
	composedContainerIds = strings.TrimRight(composedContainerIds, ",")
	signalOnKafka(composedMinioKey, composedContainerIds)
	fmt.Fprintf(w, "Stopped collecting")
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
		    fmt.Println(fmt.Sprint(err) + ": " + stderr.String())
		    return
		}
		fmt.Println("Result: " + out.String())
	}

func main() {
	collecting = false
	
	http.HandleFunc("/start", startCollecting)
	http.HandleFunc("/stop", stopCollecting)
	http.ListenAndServe(":8080", nil)
}