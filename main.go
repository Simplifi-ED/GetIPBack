package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/charmbracelet/log"
)

var subscriptionId string
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

func main() {
	subscriptionId = os.Getenv("AZURE_SUBSCRIPTION_ID")
	if len(subscriptionId) == 0 {
		log.Fatal("AZURE_SUBSCRIPTION_ID is not set.")
	}

	log.Info("Creating VMs...")

	numJobs, err := strconv.Atoi(os.Getenv("DETECTIVE_CONCURRENT_JOBS"))
	if err != nil {
		fmt.Println("Error during conversion")
		return
	}

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

	log.Info("Assiging Public IPs...")

	log.Info("Running Jobs...")
	var wgPIP sync.WaitGroup
	tasks := make(chan int)

	for i := 0; i < numJobs; i++ {
		wgPIP.Add(1)
		go associatePublicIP(i, tasks, &wgPIP)
	}

	for i := 1; i <= 10; i++ {
		tasks <- i
	}

	// Close the task channel to signal that no more tasks will be added.
	close(tasks)

	// Wait for all worker goroutines to finish.
	wgPIP.Wait()

}

func associatePublicIP(jobID int, tasks <-chan int, wg *sync.WaitGroup) {
	subscriptionId = os.Getenv("AZURE_SUBSCRIPTION_ID")
	if len(subscriptionId) == 0 {
		log.Fatal("AZURE_SUBSCRIPTION_ID is not set.")
	}

	ctx := context.Background()

	defer wg.Done()

	for task := range tasks {
		_ = task
		publicIP, err := createPublicIP(ctx, jobID)
		if err != nil {
			log.Fatalf("cannot create public IP address:%+v", err)
		}
		log.Info("Created public IP address", "PublicIPid", *publicIP.ID)

		vmNic, err := interfacesClient.Get(context.Background(), resourceGroupName, fmt.Sprintf("%s-%d", nicName, jobID), nil)
		if err != nil {
			log.Fatal(err)
		}
		vmSubnet, err := subnetsClient.Get(context.Background(), resourceGroupName, fmt.Sprintf("%s-%d", vnetName, jobID), fmt.Sprintf("%s-%d", subnetName, jobID), nil)
		if err != nil {
			log.Fatal(err)
		}
		parameters := armnetwork.Interface{
			Location: to.Ptr(location),
			Properties: &armnetwork.InterfacePropertiesFormat{
				IPConfigurations: []*armnetwork.InterfaceIPConfiguration{
					{
						Name: to.Ptr("ipConfig"),
						Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
							PublicIPAddress: &armnetwork.PublicIPAddress{
								ID: to.Ptr(*publicIP.ID),
							},
							PrivateIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodDynamic),
							Subnet: &armnetwork.Subnet{
								ID: to.Ptr(*vmSubnet.ID),
							},
						},
					},
				},
			},
		}
		if err != nil {
			log.Fatal(err)
		}

		pollerResponse, err := interfacesClient.BeginCreateOrUpdate(ctx, resourceGroupName, *vmNic.Name, parameters, nil)
		if err != nil {
			log.Fatal(err)
		}

		resp, err := pollerResponse.PollUntilDone(ctx, nil)
		if err != nil {
			log.Fatal(err)
		}

		log.Info("Public IP Associated", "NicName", *resp.Name)

		time.Sleep(10 * time.Second)

		allocatedIP := getPublicIP(jobID)

		if allocatedIP == desiredIP {
			log.Info("Allocated IP address matches the desired IP address. \n", "Job", jobID, "IP", allocatedIP)

		}
		if allocatedIP != desiredIP {
			log.Info("Allocated IP address does not match the desired IP address. \n", "Job", jobID, "AllocatedIP", allocatedIP, "DesiredIP", desiredIP)
			dissociateAndDeletePublicIP(ctx, jobID)
		}

	}

}

func dissociateAndDeletePublicIP(ctx context.Context, jobID int) {
	vmNic, err := interfacesClient.Get(context.Background(), resourceGroupName, fmt.Sprintf("%s-%d", nicName, jobID), nil)
	if err != nil {
		log.Fatal(err)
	}
	vmSubnet, err := subnetsClient.Get(context.Background(), resourceGroupName, fmt.Sprintf("%s-%d", vnetName, jobID), fmt.Sprintf("%s-%d", subnetName, jobID), nil)
	if err != nil {
		log.Fatal(err)
	}
	parameters := armnetwork.Interface{
		Location: to.Ptr(location),
		Properties: &armnetwork.InterfacePropertiesFormat{
			IPConfigurations: []*armnetwork.InterfaceIPConfiguration{
				{
					Name: to.Ptr("ipConfig"),
					Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
						PrivateIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodDynamic),
						Subnet: &armnetwork.Subnet{
							ID: to.Ptr(*vmSubnet.ID),
						},
					},
				},
			},
		},
	}
	if err != nil {
		log.Fatal(err)
	}

	pollerResponse, err := interfacesClient.BeginCreateOrUpdate(ctx, resourceGroupName, *vmNic.Name, parameters, nil)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := pollerResponse.PollUntilDone(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	log.Info("Public IP Disassociated", "NicName", *resp.Name)

	err = deletePublicIP(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot delete public IP address:%+v", err)
	}
	log.Info("deleted public IP address")

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

	log.Info("start creating virtual machine...")
	virtualNetwork, err := createVirtualNetwork(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot create virtual network:%+v", err)
	}
	log.Info("Created Vnet", "VirtualNetworkID", *virtualNetwork.ID)

	subnet, err := createSubnets(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot create subnet:%+v", err)
	}
	log.Info("Created subnet", "SubnetID", *subnet.ID)

	netWorkInterface, err := createNetWorkInterface(ctx, *subnet.ID, jobID)
	if err != nil {
		log.Fatalf("cannot create network interface:%+v", err)
	}
	log.Info("Created network interface", "NicID", *netWorkInterface.ID)

	networkInterfaceID := netWorkInterface.ID
	virtualMachine, err := createVirtualMachine(ctx, *networkInterfaceID, jobID)
	if err != nil {
		log.Fatalf("cannot create virual machine:%+v", err)
	}
	log.Info("Created network virual machine", "vmID", *virtualMachine.ID)

	resultChan <- fmt.Sprintf("Virtual machine created successfully", "Job", jobID)

}

func cleanup(jobID int) {
	ctx := context.Background()

	log.Warn("start deleting virtual machine...")
	err := deleteVirtualMachine(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot delete virtual machine:%+v", err)
	}
	log.Warn("deleted virtual machine")

	err = deleteDisk(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot delete disk:%+v", err)
	}
	log.Warn("deleted disk")

	err = deleteNetWorkInterface(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot delete network interface:%+v", err)
	}
	log.Warn("deleted network interface")

	err = deletePublicIP(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot delete public IP address:%+v", err)
	}
	log.Warn("deleted public IP address")

	err = deleteSubnets(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot delete subnet:%+v", err)
	}
	log.Warn("deleted subnet")

	err = deleteVirtualNetWork(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot delete virtual network:%+v", err)
	}
	log.Warn("deleted virtual network")

	log.Info("success deleted virtual machine.")
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

func createNetWorkInterface(ctx context.Context, subnetID string, x int) (*armnetwork.Interface, error) {

	parameters := armnetwork.Interface{
		Location: to.Ptr(location),
		Properties: &armnetwork.InterfacePropertiesFormat{
			IPConfigurations: []*armnetwork.InterfaceIPConfiguration{
				{
					Name: to.Ptr("ipConfig"),
					Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
						PrivateIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodDynamic),
						Subnet: &armnetwork.Subnet{
							ID: to.Ptr(subnetID),
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
	parameters := armcompute.VirtualMachine{
		Location: to.Ptr(location),
		Identity: &armcompute.VirtualMachineIdentity{
			Type: to.Ptr(armcompute.ResourceIdentityTypeNone),
		},
		Properties: &armcompute.VirtualMachineProperties{
			StorageProfile: &armcompute.StorageProfile{
				ImageReference: &armcompute.ImageReference{
					Offer:     to.Ptr("0001-com-ubuntu-server-jammy"),
					Publisher: to.Ptr("Canonical"),
					SKU:       to.Ptr("22_04-lts-arm64"),
					Version:   to.Ptr("22.04.202310260"),
				},
				OSDisk: &armcompute.OSDisk{
					Name:         to.Ptr(fmt.Sprintf("%s-%d", diskName, x)),
					CreateOption: to.Ptr(armcompute.DiskCreateOptionTypesFromImage),
					Caching:      to.Ptr(armcompute.CachingTypesReadWrite),
					ManagedDisk: &armcompute.ManagedDiskParameters{
						StorageAccountType: to.Ptr(armcompute.StorageAccountTypesStandardLRS), // OSDisk type Standard/Premium HDD/SSD
					},
					//DiskSizeGB: to.Ptr[int32](100), // default 127G
				},
			},
			Priority: to.Ptr(armcompute.VirtualMachinePriorityTypesSpot),
			HardwareProfile: &armcompute.HardwareProfile{
				VMSize: to.Ptr(armcompute.VirtualMachineSizeTypes("Standard_B2pts_v2")), // Standard_B2pts_v2
			},
			OSProfile: &armcompute.OSProfile{
				ComputerName:  to.Ptr(fmt.Sprintf("%s-%d", vmName, x)),
				AdminUsername: to.Ptr("azureuser"),
				AdminPassword: to.Ptr("Password01!@#"),
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

	pollerResponse, err := virtualMachinesClient.BeginCreateOrUpdate(ctx, resourceGroupName, fmt.Sprintf("%s-%d", vmName, x), parameters, nil)
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
