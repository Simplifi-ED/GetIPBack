# GetIPBack

## Description
The program runs N VMs of size Standard_B2pts_v2 which represents the workers and N iterations.

## Workers and Iterations
### Adjusting the workers
```
log.Info("Creating VMs...")
numJobs := 5 // Number of VMs

var wg sync.WaitGroup
resultChan := make(chan string, numJobs)

for i := 0; i < numJobs; i++ {
    wg.Add(1)
    go createVM(&wg, i, resultChan)
}
```
### Adjusting the iterations
```
log.Info("Running Jobs...")
var wgPIP sync.WaitGroup
tasks := make(chan int)

for i := 0; i < numJobs; i++ {
    wgPIP.Add(1)
    go associatePublicIP(i, tasks, &wgPIP)
}

for i := 1; i <= 10; i++ { //Number of Iterations
    tasks <- i
}
```

## Requirements
```
go1.19.13
```

## Running the program
```
export AZURE_SUBSCRIPTION_ID=....
```

```
# go run main.go
```