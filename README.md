# GetIPBack


## TL&DR

### Description
The program runs N VMs of size Standard_B2pts_v2 which represents the workers and N iterations and stops when the ip is found.

## Installation
get the go binaries from releases:

```shell
curl -Lso https://github.com/Simplifi-ED/GetIPBack/releases/download/v0.0.5/GetIPBack_0.0.5_linux_amd64.tar.gz
tar -xcf GetIPBack_0.0.5_linux_amd64.tar.gz
(sudo) mv GetIPBack_0.0.5_linux_amd64 /usr/local/bin/getipback
(sudo) chmod +x /usr/local/bin/getipback
```

## Executing
Export the env variables:

```
# env to set before starting the process
export DETECTIVE_RG=
export DETECTIVE_VM_NAME=
export DETECTIVE_VNET_NAME=
export DETECTIVE_SNET_NAME=
export DETECTIVE_NIC_NAME=
export DETECTIVE_DISK_NAME=
export DETECTIVE_PIP_NAME=
export DETECTIVE_LOCATION=
export DETECTIVE_MAGIC_IP=
export DETECTIVE_NUM_ITERATION=
export DETECTIVE_CONCURRENT_JOBS=
export AZURE_SUBSCRIPTION_ID=
```

login to azure 

```shell
az login
```
Run the binary
```shell
GetIPBack
```

## Optional flags
### spot
default to **true**
```shell
GetIPBack -spot=false
```
### logpath
default to **/usr/local/var/log/IPBack**
```shell
GetIPBack -logpath="your/log/path"
```
