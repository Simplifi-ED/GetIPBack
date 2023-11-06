// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

var subscriptionId string

const (
	resourceGroupName = "rg-marouane"
	vmName            = "happybit-vm"
	vnetName          = "happybit-vnet"
	subnetName        = "happybit-subnet"
	nicName           = "happybit-nic"
	diskName          = "happybit-disk"
	publicIPName      = "happybit-public-ip"
	location          = "francecentral"
	desiredIP         = "52.143.143.195"
)

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

func main() {
	subscriptionId = os.Getenv("AZURE_SUBSCRIPTION_ID")
	if len(subscriptionId) == 0 {
		log.Fatal("AZURE_SUBSCRIPTION_ID is not set.")
	}
	fmt.Printf("Running jobs...")
	numJobs := 10

	var wg sync.WaitGroup
	resultChan := make(chan string, numJobs)

	for i := 0; i < numJobs; i++ {
		wg.Add(1)
		go createVM(&wg, i, resultChan)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for result := range resultChan {
		fmt.Println(result)
	}

}

func createVM(wg *sync.WaitGroup, jobID int, resultChan chan string) {
	defer wg.Done()
	conn, err := connectionAzure()
	if err != nil {
		log.Fatalf("cannot connect to Azure:%+v", err)
	}
	ctx := context.Background()

	resourcesClientFactory, err = armresources.NewClientFactory(subscriptionId, conn, nil)
	if err != nil {
		log.Fatal(err)
	}
	networkClientFactory, err = armnetwork.NewClientFactory(subscriptionId, conn, nil)
	if err != nil {
		log.Fatal(err)
	}
	virtualNetworksClient = networkClientFactory.NewVirtualNetworksClient()
	subnetsClient = networkClientFactory.NewSubnetsClient()
	publicIPAddressesClient = networkClientFactory.NewPublicIPAddressesClient()
	interfacesClient = networkClientFactory.NewInterfacesClient()

	computeClientFactory, err = armcompute.NewClientFactory(subscriptionId, conn, nil)
	if err != nil {
		log.Fatal(err)
	}
	virtualMachinesClient = computeClientFactory.NewVirtualMachinesClient()
	disksClient = computeClientFactory.NewDisksClient()

	log.Println("start creating virtual machine...")
	virtualNetwork, err := createVirtualNetwork(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot create virtual network:%+v", err)
	}
	log.Printf("Created virtual network: %s", *virtualNetwork.ID)

	subnet, err := createSubnets(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot create subnet:%+v", err)
	}
	log.Printf("Created subnet: %s", *subnet.ID)

	publicIP, err := createPublicIP(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot create public IP address:%+v", err)
	}
	log.Printf("Created public IP address: %s", *publicIP.ID)

	netWorkInterface, err := createNetWorkInterface(ctx, *subnet.ID, *publicIP.ID, jobID)
	if err != nil {
		log.Fatalf("cannot create network interface:%+v", err)
	}
	log.Printf("Created network interface: %s", *netWorkInterface.ID)

	networkInterfaceID := netWorkInterface.ID
	virtualMachine, err := createVirtualMachine(ctx, *networkInterfaceID, jobID)
	if err != nil {
		log.Fatalf("cannot create virual machine:%+v", err)
	}
	log.Printf("Created network virual machine: %s", *virtualMachine.ID)

	log.Println("Virtual machine created successfully")
	fmt.Println("Waiting for Public IP address to be allocated...")
	time.Sleep(10 * time.Second)
	allocatedIP := getPublicIP(jobID)
	if allocatedIP == desiredIP {
		resultChan <- fmt.Sprintf("Job %d: Allocated IP address matches the desired IP address: %s", jobID, allocatedIP)
	}
	if allocatedIP != desiredIP {
		resultChan <- fmt.Sprintf("Job %d: Allocated IP address (%s) does not match the desired IP address (%s).", jobID, allocatedIP, desiredIP)
		cleanup(jobID)
	}
}

func cleanup(jobID int) {
	ctx := context.Background()

	log.Println("start deleting virtual machine...")
	err := deleteVirtualMachine(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot delete virtual machine:%+v", err)
	}
	log.Println("deleted virtual machine")

	err = deleteDisk(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot delete disk:%+v", err)
	}
	log.Println("deleted disk")

	err = deleteNetWorkInterface(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot delete network interface:%+v", err)
	}
	log.Println("deleted network interface")

	err = deletePublicIP(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot delete public IP address:%+v", err)
	}
	log.Println("deleted public IP address")

	err = deleteSubnets(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot delete subnet:%+v", err)
	}
	log.Println("deleted subnet")

	err = deleteVirtualNetWork(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot delete virtual network:%+v", err)
	}
	log.Println("deleted virtual network")

	log.Println("success deleted virtual machine.")
}

func getPublicIP(x int) string {
	resp, err := publicIPAddressesClient.Get(context.Background(), resourceGroupName, fmt.Sprintf("%s-%d", publicIPName, x), nil)
	if err != nil {
		log.Fatalf("failed to get public IP address: %v", err)
	}
	ipAddress := *resp.PublicIPAddress.Properties.IPAddress
	return ipAddress
}
func connectionAzure() (azcore.TokenCredential, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, err
	}
	return cred, nil
}

func createVirtualNetwork(ctx context.Context, x int) (*armnetwork.VirtualNetwork, error) {

	parameters := armnetwork.VirtualNetwork{
		Location: to.Ptr(location),
		Properties: &armnetwork.VirtualNetworkPropertiesFormat{
			AddressSpace: &armnetwork.AddressSpace{
				AddressPrefixes: []*string{
					to.Ptr("10.1.0.0/16"),
				},
			},
		},
	}

	pollerResponse, err := virtualNetworksClient.BeginCreateOrUpdate(ctx, resourceGroupName, fmt.Sprintf("%s-%d", vnetName, x), parameters, nil)
	if err != nil {
		return nil, err
	}

	resp, err := pollerResponse.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &resp.VirtualNetwork, nil
}

func deleteVirtualNetWork(ctx context.Context, x int) error {

	pollerResponse, err := virtualNetworksClient.BeginDelete(ctx, resourceGroupName, fmt.Sprintf("%s-%d", vnetName, x), nil)
	if err != nil {
		return err
	}

	_, err = pollerResponse.PollUntilDone(ctx, nil)
	if err != nil {
		return err
	}

	return nil
}

func createSubnets(ctx context.Context, x int) (*armnetwork.Subnet, error) {

	parameters := armnetwork.Subnet{
		Properties: &armnetwork.SubnetPropertiesFormat{
			AddressPrefix: to.Ptr("10.1.10.0/24"),
		},
	}

	pollerResponse, err := subnetsClient.BeginCreateOrUpdate(ctx, resourceGroupName, fmt.Sprintf("%s-%d", vnetName, x), fmt.Sprintf("%s-%d", subnetName, x), parameters, nil)
	if err != nil {
		return nil, err
	}

	resp, err := pollerResponse.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &resp.Subnet, nil
}

func deleteSubnets(ctx context.Context, x int) error {

	pollerResponse, err := subnetsClient.BeginDelete(ctx, resourceGroupName, fmt.Sprintf("%s-%d", vnetName, x), subnetName, nil)
	if err != nil {
		return err
	}

	_, err = pollerResponse.PollUntilDone(ctx, nil)
	if err != nil {
		return err
	}

	return nil
}

func createPublicIP(ctx context.Context, x int) (*armnetwork.PublicIPAddress, error) {

	parameters := armnetwork.PublicIPAddress{
		Location: to.Ptr(location),
		Properties: &armnetwork.PublicIPAddressPropertiesFormat{
			PublicIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodDynamic), // Static or Dynamic
		},
	}

	pollerResponse, err := publicIPAddressesClient.BeginCreateOrUpdate(ctx, resourceGroupName, fmt.Sprintf("%s-%d", publicIPName, x), parameters, nil)
	if err != nil {
		return nil, err
	}

	resp, err := pollerResponse.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &resp.PublicIPAddress, err
}

func deletePublicIP(ctx context.Context, x int) error {

	pollerResponse, err := publicIPAddressesClient.BeginDelete(ctx, resourceGroupName, fmt.Sprintf("%s-%d", publicIPName, x), nil)
	if err != nil {
		return err
	}

	_, err = pollerResponse.PollUntilDone(ctx, nil)
	if err != nil {
		return err
	}
	return nil
}

func createNetWorkInterface(ctx context.Context, subnetID string, publicIPID string, x int) (*armnetwork.Interface, error) {

	parameters := armnetwork.Interface{
		Location: to.Ptr(location),
		Properties: &armnetwork.InterfacePropertiesFormat{
			//NetworkSecurityGroup:
			IPConfigurations: []*armnetwork.InterfaceIPConfiguration{
				{
					Name: to.Ptr("ipConfig"),
					Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
						PrivateIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodDynamic),
						Subnet: &armnetwork.Subnet{
							ID: to.Ptr(subnetID),
						},
						PublicIPAddress: &armnetwork.PublicIPAddress{
							ID: to.Ptr(publicIPID),
						},
					},
				},
			},
		},
	}

	pollerResponse, err := interfacesClient.BeginCreateOrUpdate(ctx, resourceGroupName, fmt.Sprintf("%s-%d", nicName, x), parameters, nil)
	if err != nil {
		return nil, err
	}

	resp, err := pollerResponse.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &resp.Interface, err
}

func deleteNetWorkInterface(ctx context.Context, x int) error {

	pollerResponse, err := interfacesClient.BeginDelete(ctx, resourceGroupName, fmt.Sprintf("%s-%d", nicName, x), nil)
	if err != nil {
		return err
	}

	_, err = pollerResponse.PollUntilDone(ctx, nil)
	if err != nil {
		return err
	}

	return nil
}

func createVirtualMachine(ctx context.Context, networkInterfaceID string, x int) (*armcompute.VirtualMachine, error) {
	//require ssh key for authentication on linux
	// sshPublicKeyPath := "~/.ssh/id_rsa.pub"
	// var sshBytes []byte
	// _, err := os.Stat(sshPublicKeyPath)
	// if err == nil {
	// 	sshBytes, err = os.ReadFile(sshPublicKeyPath)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// }

	parameters := armcompute.VirtualMachine{
		Location: to.Ptr(location),
		Identity: &armcompute.VirtualMachineIdentity{
			Type: to.Ptr(armcompute.ResourceIdentityTypeNone),
		},
		Properties: &armcompute.VirtualMachineProperties{
			StorageProfile: &armcompute.StorageProfile{
				ImageReference: &armcompute.ImageReference{
					// search image reference
					// az vm image list --output table
					// Offer:     to.Ptr("WindowsServer"),
					// Publisher: to.Ptr("MicrosoftWindowsServer"),
					// SKU:       to.Ptr("2019-Datacenter"),
					// Version:   to.Ptr("latest"),
					// require ssh key for authentication on linux
					Offer:     to.Ptr("UbuntuServer"),
					Publisher: to.Ptr("Canonical"),
					SKU:       to.Ptr("18.04-LTS"),
					Version:   to.Ptr("latest"),
				},
				OSDisk: &armcompute.OSDisk{
					Name:         to.Ptr(fmt.Sprintf("%s-%s", diskName, string(x))),
					CreateOption: to.Ptr(armcompute.DiskCreateOptionTypesFromImage),
					Caching:      to.Ptr(armcompute.CachingTypesReadWrite),
					ManagedDisk: &armcompute.ManagedDiskParameters{
						StorageAccountType: to.Ptr(armcompute.StorageAccountTypesStandardLRS), // OSDisk type Standard/Premium HDD/SSD
					},
					//DiskSizeGB: to.Ptr[int32](100), // default 127G
				},
			},
			HardwareProfile: &armcompute.HardwareProfile{
				VMSize: to.Ptr(armcompute.VirtualMachineSizeTypes("Standard_B1ls")), // VM size include vCPUs,RAM,Data Disks,Temp storage.
			},
			OSProfile: &armcompute.OSProfile{ //
				ComputerName:  to.Ptr(fmt.Sprintf("%s-%s", vmName, string(x))),
				AdminUsername: to.Ptr("azureuser"),
				AdminPassword: to.Ptr("Password01!@#"),
				//require ssh key for authentication on linux
				// LinuxConfiguration: &armcompute.LinuxConfiguration{
				// 	DisablePasswordAuthentication: to.Ptr(true),
				// 	SSH: &armcompute.SSHConfiguration{
				// 		PublicKeys: []*armcompute.SSHPublicKey{
				// 			{
				// 				Path:    to.Ptr(fmt.Sprintf("/home/%s/.ssh/authorized_keys", "azureuser")),
				// 				KeyData: to.Ptr(string(sshBytes)),
				// 			},
				// 		},
				// 	},
				// },
			},
			NetworkProfile: &armcompute.NetworkProfile{
				NetworkInterfaces: []*armcompute.NetworkInterfaceReference{
					{
						ID: to.Ptr(networkInterfaceID),
					},
				},
			},
		},
	}

	pollerResponse, err := virtualMachinesClient.BeginCreateOrUpdate(ctx, resourceGroupName, fmt.Sprintf("%s-%s", vmName, string(x)), parameters, nil)
	if err != nil {
		return nil, err
	}

	resp, err := pollerResponse.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &resp.VirtualMachine, nil
}

func deleteVirtualMachine(ctx context.Context, x int) error {

	pollerResponse, err := virtualMachinesClient.BeginDelete(ctx, resourceGroupName, fmt.Sprintf("%s-%d", vmName, x), nil)
	if err != nil {
		return err
	}

	_, err = pollerResponse.PollUntilDone(ctx, nil)
	if err != nil {
		return err
	}

	return nil
}

func deleteDisk(ctx context.Context, x int) error {

	pollerResponse, err := disksClient.BeginDelete(ctx, resourceGroupName, fmt.Sprintf("%s-%d", diskName, x), nil)
	if err != nil {
		return err
	}

	_, err = pollerResponse.PollUntilDone(ctx, nil)
	if err != nil {
		return err
	}
	return nil
}
