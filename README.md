# GetIPBack


## TL&DR

get the go binaries from releases:

```shell
curl -Lso https://github.com/Simplifi-ED/GetIPBack/releases/download/v0.0.1-rc1/GetIPBack_0.0.1-rc1_linux_amd64.tar.gz
tar -xcf GetIPBack_0.0.1-rc1_linux_amd64.tar.gz
(sudo) mv GetIPBack_0.0.1-rc1_linux_amd64 /usr/local/bin/GetIPBack
(sudo) chmod +x /usr/local/bin/GetIPBack
```

Export the env variables:

```
# env to set before starting the process
DETECTIVE_RG=
DETECTIVE_VM_NAME=
DETECTIVE_VNET_NAME=
DETECTIVE_SNET_NAME=
DETECTIVE_NIC_NAME=
DETECTIVE_DISK_NAME=
DETECTIVE_PIP_NAME=
DETECTIVE_LOCATION=
DETECTIVE_MAGIC_IP=
DETECTIVE_NUM_ITERATION=
DETECTIVE_CONCURRENT_JOBS=
AZURE_SUBSCRIPTION_ID=
```

then, 

```shell
az login
GetIPBack
```

## Description
The program runs N VMs of size Standard_B2pts_v2 which represents the workers and N iterations.
