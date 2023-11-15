package app

import (
	"context"
	"fmt"
	"os"
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

func AssociatePublicIP(ctx context.Context, jobID int, tasks <-chan int, wg *sync.WaitGroup) {
	SubscriptionId = os.Getenv("AZURE_SUBSCRIPTION_ID")
	if len(SubscriptionId) == 0 {
		log.Fatal("AZURE_SUBSCRIPTION_ID is not set.")
	}

	lctx := context.Background()

	defer wg.Done()

	for task := range tasks {
		_ = task
		publicIP, err := createPublicIP(lctx, jobID)
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
		success := false
		for !success {
			pollerResponse, err := interfacesClient.BeginCreateOrUpdate(ctx, resourceGroupName, *vmNic.Name, parameters, nil)
			if err != nil {
				if IsThrottlingError(err) {
					log.Warn(fmt.Sprintf("Job: %d - Too Many Requests. Retrying after 303 seconds...", jobID))
					time.Sleep(304 * time.Second)
					continue // Retry the operation
				} else {
					log.Fatal(err)
				}
			}

			resp, err := pollerResponse.PollUntilDone(ctx, nil)
			if err != nil {
				log.Fatal(err)
			}
			success = true
			log.Info("Public IP Associated", "NicName", *resp.Name)
			break
		}

		time.Sleep(10 * time.Second)
		select {
		case <-ctx.Done():
			return
		default:
		}
		allocatedIP := getPublicIP(jobID)

		if allocatedIP == desiredIP {
			IPBackLog.Info(fmt.Sprintf("Job %d: Allocated IP address matches the desired IP address: %s  \x1b[32m[Success]\n", jobID, allocatedIP))
			log.Info("Allocated IP address matches the desired IP address. \n", "Job", jobID, "IP", allocatedIP)
			Cancel()
			return
		}
		if allocatedIP != desiredIP {
			IPBackLog.Info(fmt.Sprintf("Job %d: Allocated IP address (%s) does not match the desired IP address (%s). \n", jobID, allocatedIP, desiredIP))
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

	success := false
	for !success {
		pollerResponse, err := interfacesClient.BeginCreateOrUpdate(ctx, resourceGroupName, *vmNic.Name, parameters, nil)
		if err != nil {
			if IsThrottlingError(err) {
				log.Warn(fmt.Sprintf("Job: %d - Too Many Requests. Retrying after 303 seconds...", jobID))
				time.Sleep(304 * time.Second)
				continue // Retry the operation
			} else {
				log.Fatal(err)
			}
		}

		resp, err := pollerResponse.PollUntilDone(ctx, nil)
		if err != nil {
			log.Fatal(err)
		}
		success = true
		log.Info("Public IP Disassociated", "NicName", *resp.Name)
		break
	}

	err = deletePublicIP(ctx, jobID)
	if err != nil {
		log.Fatalf("cannot delete public IP address:%+v", err)
	}
	log.Info("Public IP address deleted", "PublicIpName", fmt.Sprintf("%s-%d", publicIPName, jobID))

}

func CreateVM(wg *sync.WaitGroup, jobID int, resultChan chan string) {
	defer wg.Done()
	conn, err := connectionAzure()
	if err != nil {
		log.Fatalf("cannot connect to Azure:%+v", err)
	}
	ctx := context.Background()

	resourcesClientFactory, err = armresources.NewClientFactory(SubscriptionId, conn, nil)
	if err != nil {
		log.Fatal(err)
	}
	networkClientFactory, err = armnetwork.NewClientFactory(SubscriptionId, conn, nil)
	if err != nil {
		log.Fatal(err)
	}
	virtualNetworksClient = networkClientFactory.NewVirtualNetworksClient()
	subnetsClient = networkClientFactory.NewSubnetsClient()
	publicIPAddressesClient = networkClientFactory.NewPublicIPAddressesClient()
	interfacesClient = networkClientFactory.NewInterfacesClient()
	computeClientFactory, err = armcompute.NewClientFactory(SubscriptionId, conn, nil)
	if err != nil {
		log.Fatal(err)
	}
	virtualMachinesClient = computeClientFactory.NewVirtualMachinesClient()
	disksClient = computeClientFactory.NewDisksClient()
	log.Info(fmt.Sprintf("Job: %d start creating virtual machine (%s)...", jobID, fmt.Sprintf("%s-%d", vmName, jobID)))
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

	resultChan <- fmt.Sprintf("Job %d Virtual machine created successfully.", jobID)

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

	for {
		pollerResponse, err := publicIPAddressesClient.BeginCreateOrUpdate(ctx, resourceGroupName, fmt.Sprintf("%s-%d", publicIPName, x), parameters, nil)
		if err != nil {
			if IsThrottlingError(err) {
				log.Warn(fmt.Sprintf("Job: %d - Too Many Requests. Retrying after 303 seconds...", x))
				time.Sleep(304 * time.Second)
				continue // Retry the operation
			} else {
				return nil, err
			}
		}

		resp, err := pollerResponse.PollUntilDone(ctx, nil)
		if err != nil {
			return nil, err
		}

		return &resp.PublicIPAddress, nil
	}
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
	Priority := to.Ptr(armcompute.VirtualMachinePriorityTypesSpot)
	if !*Spot {
		Priority = to.Ptr(armcompute.VirtualMachinePriorityTypesRegular)
	}
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
					// Offer:     to.Ptr("UbuntuServer"),
					// Publisher: to.Ptr("Canonical"),
					// SKU:       to.Ptr("18.04-LTS"),
					// Version:   to.Ptr("latest"),
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
			Priority: Priority,
			HardwareProfile: &armcompute.HardwareProfile{
				VMSize: to.Ptr(armcompute.VirtualMachineSizeTypes("Standard_B2pts_v2")), // Standard_B1ls
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
