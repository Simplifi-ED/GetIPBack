package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	app "github.com/Simplifi-ED/getipback/internal/app"
	"github.com/charmbracelet/log"
)

func main() {
	app.Gctx, app.Cancel = context.WithCancel(context.Background())
	defer app.Cancel()
	app.Spot = flag.Bool("spot", true, "Specify if spot is true or false")
	logdirPath := flag.String("logpath", "/usr/local/var/log/IPBack", "Specify logs directory path")
	flag.Parse()
	if _, err := os.Stat(*logdirPath); os.IsNotExist(err) {
		err := os.MkdirAll(*logdirPath, 0755)
		if err != nil {
			log.Fatal("Error creating directory:", "Error", err)
		}
	}
	IPBackLogFile, err := app.OpenLogFile(fmt.Sprintf("%s/detective-ip.log", *logdirPath))
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to open log file [%v.]", err))
	}
	defer IPBackLogFile.Close()
	app.IPBackLog = log.NewWithOptions(os.Stderr, log.Options{
		ReportCaller:    false,
		ReportTimestamp: true,
		TimeFormat:      time.Kitchen,
	})
	app.IPBackLog.SetOutput(IPBackLogFile)
	app.SubscriptionId = os.Getenv("AZURE_SUBSCRIPTION_ID")
	if len(app.SubscriptionId) == 0 {
		log.Fatal("AZURE_SUBSCRIPTION_ID is not set.")
	}

	log.Info("Creating VMs...")

	numJobs, err := strconv.Atoi(os.Getenv("DETECTIVE_CONCURRENT_JOBS"))
	if err != nil {
		fmt.Println("Error getting DETECTIVE_CONCURRENT_JOBS")
		return
	}

	var wg sync.WaitGroup
	resultChan := make(chan string, numJobs)

	for i := 0; i < numJobs; i++ {
		wg.Add(1)
		go app.CreateVM(&wg, i, resultChan)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for result := range resultChan {
		log.Info(result)
	}

	log.Info("Assiging Public IPs...")

	log.Info("Running Jobs...")
	var wgPIP sync.WaitGroup
	tasks := make(chan int)

	for i := 0; i < numJobs; i++ {
		wgPIP.Add(1)
		go app.AssociatePublicIP(app.Gctx, i, tasks, &wgPIP)
	}

	numIterations, err := strconv.Atoi(os.Getenv("DETECTIVE_NUM_ITERATION"))
	if err != nil {
		log.Fatal("Error getting DETECTIVE_NUM_ITERATION")
		return
	}
	for i := 1; i <= numIterations; i++ {
		tasks <- i
	}

	// Close the task channel to signal that no more tasks will be added.
	close(tasks)

	// Wait for all worker goroutines to finish.
	wgPIP.Wait()

}
