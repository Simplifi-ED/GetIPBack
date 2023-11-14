package app

import (
	"context"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/charmbracelet/log"
)

var SubscriptionId string
var NumIterations int
var IPBackLog *log.Logger
var Spot *bool
var Cancel context.CancelFunc
var Gctx context.Context
var resourceGroupName string = os.Getenv("DETECTIVE_RG")
var vmName string = os.Getenv("DETECTIVE_VM_NAME")
var vnetName string = os.Getenv("DETECTIVE_VNET_NAME")
var subnetName string = os.Getenv("DETECTIVE_SNET_NAME")
var nicName string = os.Getenv("DETECTIVE_NIC_NAME")
var diskName string = os.Getenv("DETECTIVE_DISK_NAME")
var publicIPName string = os.Getenv("DETECTIVE_PIP_NAME")
var location string = os.Getenv("DETECTIVE_LOCATION")
var desiredIP string = os.Getenv("DETECTIVE_MAGIC_IP")

var (
	resourcesClientFactory *armresources.ClientFactory
	computeClientFactory   *armcompute.ClientFactory
	networkClientFactory   *armnetwork.ClientFactory
)

var (
	virtualNetworksClient   *armnetwork.VirtualNetworksClient
	subnetsClient           *armnetwork.SubnetsClient
	publicIPAddressesClient *armnetwork.PublicIPAddressesClient
	interfacesClient        *armnetwork.InterfacesClient

	virtualMachinesClient *armcompute.VirtualMachinesClient
	disksClient           *armcompute.DisksClient
)
