package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/gorilla/mux"
	consul "github.com/hashicorp/consul/api"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/mem"
)

var consulClient *consul.Client

const serviceName = "carbonifer-stress"
const exposedPort = 8080

var stressCmd *exec.Cmd
var stderr bytes.Buffer

type Stats struct {
	CPU     float64 `json:"cpu"`
	Memory  uint64  `json:"memory"`
	Storage uint64  `json:"storage"`
}

func setupConsulClient() {
	consulAgent := "consul-agent:8500"

	// Read environment variable if available
	if consulAgentEnv, ok := os.LookupEnv("CONSUL_AGENT"); ok {
		consulAgent = consulAgentEnv
	}

	consulConfig := consul.DefaultConfig()
	consulConfig.Address = consulAgent

	var err error
	consulClient, err = consul.NewClient(consulConfig)
	if err != nil {
		log.Fatal("Failed to connect to consul: ", err)
	}
}

func registerService() {
	reg := &consul.AgentServiceRegistration{
		ID:   serviceName,
		Name: serviceName,
		Port: exposedPort,
	}

	err := consulClient.Agent().ServiceRegister(reg)
	if err != nil {
		log.Fatal("Failed to register service: ", err)
	}
}

func getInstances() []string {
	services, _, err := consulClient.Catalog().Service(serviceName, "", nil)
	if err != nil {
		log.Println("Failed to get services: ", err)
		return nil
	}

	instances := make([]string, len(services))
	for i, service := range services {
		instances[i] = service.Address // Using service.Address instead of service.ServiceAddress
	}
	return instances
}

func getStats() (*Stats, error) {
	stats := &Stats{}

	cpuPercent, err := cpu.Percent(0, false)
	if err != nil {
		return nil, err
	}

	if len(cpuPercent) > 0 {
		stats.CPU = cpuPercent[0]
	}

	virtualMemory, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	stats.Memory = virtualMemory.Used

	diskUsage, err := disk.Usage("/")
	if err != nil {
		return nil, err
	}

	stats.Storage = diskUsage.Used

	return stats, nil
}

func stopAllStressProcesses() {
	out, err := exec.Command("pgrep", "-f", "stress-ng").Output()
	if err != nil {
		log.Printf("Error getting stress-ng process PIDs: %v", err)
		return
	}

	pids := strings.Fields(string(out))

	for _, pidStr := range pids {
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			log.Printf("Error converting PID to integer: %v", err)
			continue
		}

		// Send SIGTERM signal to the process
		err = syscall.Kill(pid, syscall.SIGTERM)
		if err != nil {
			log.Printf("Error stopping stress-ng process with PID %d: %v", pid, err)
			continue
		}
	}

}

func stressHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Calling /stress...\n")
	vars := mux.Vars(r)
	instance := vars["instance"]

	cpu, _ := strconv.Atoi(r.URL.Query().Get("cpu"))
	ram, _ := strconv.Atoi(r.URL.Query().Get("ram"))
	storage, _ := strconv.Atoi(r.URL.Query().Get("storage"))

	// If instance is empty, apply stress to the current instance
	if instance == "" {
		// Stop any running stress-ng process
		if stressCmd != nil && stressCmd.Process != nil && stressCmd.ProcessState == nil {
			log.Println("Stopping existing stress-ng process...")
			stopAllStressProcesses()
		}

		// If cpu is 0, do not start a new stress-ng process
		if cpu == 0 && ram == 0 && storage == 0 {
			fmt.Fprintf(w, "No stress applied, current stress-ng process stopped\n")
			return
		}

		cmdStr := "stress-ng"
		cmdStr += fmt.Sprintf(" --cpu 0 --cpu-load %d", cpu)
		if ram > 0 {
			cmdStr += fmt.Sprintf(" --vm %d --vm-bytes %dM", ram, ram)
		}
		if storage > 0 {
			cmdStr += fmt.Sprintf(" --hdd %d --hdd-bytes %dM", storage, storage)
		}

		stressCmd = exec.Command("sh", "-c", cmdStr)
		stressCmd.Stderr = &stderr

		err := stressCmd.Start()
		if err != nil {
			log.Printf("Error stressing current instance: %v", err)
			log.Printf("Stderr: %s", stderr.String())
			return
		}

		go func() {
			err := stressCmd.Wait()
			if err != nil {
				log.Printf("stress-ng command finished with error: %v", err)
			} else {
				log.Println("stress-ng command finished successfully")
			}
		}()

		fmt.Fprintf(w, "Applied stress on current instance\n")

	} else {
		// Forward the stress request to the specified instance
		url := fmt.Sprintf("http://%s/stress?", instance)
		if r.URL.Query().Get("cpu") != "" {
			url += fmt.Sprintf("cpu=%d&", cpu)
		}
		if r.URL.Query().Get("ram") != "" {
			url += fmt.Sprintf("ram=%d&", ram)
		}
		if r.URL.Query().Get("storage") != "" {
			url += fmt.Sprintf("storage=%d", storage)
		}

		resp, err := http.Get(url)
		if err != nil {
			log.Printf("Error stressing instance %s: %v", instance, err)
			return
		}

		// Forward the response from the other instance
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)

		fmt.Fprintf(w, "Applied stress on instance: %s\n", instance)
		_, err = w.Write(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func usageHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	instance := vars["instance"]
	if instance == "" {
		// Return current stress info
		stats, err := getStats()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response, err := json.Marshal(stats)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_, err = w.Write(response)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		resp, err := http.Get(fmt.Sprintf("http://%s/usage", instance))
		if err != nil {
			log.Printf("Error stressing instance %s: %v", instance, err)
			return
		}

		// Forward the response from the other instance
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		_, err = w.Write(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

}

func instancesHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Callng /instances...\n")
	instances := getInstances()
	fmt.Fprintf(w, "Instances: %s\n", strings.Join(instances, ", "))
}

func main() {
	log.Println("Registering service...")
	setupConsulClient()
	registerService()
	log.Println("Starting server...")
	router := mux.NewRouter()
	router.HandleFunc("/instances", instancesHandler)
	router.HandleFunc("/stress/{instance}", stressHandler)
	router.HandleFunc("/stress", stressHandler)
	router.HandleFunc("/usage/{instance}", usageHandler)
	router.HandleFunc("/usage", usageHandler)
	err := http.ListenAndServe(fmt.Sprintf(":%v", exposedPort), router)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
