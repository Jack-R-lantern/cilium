// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.
// Code generated by Microsoft (R) AutoRest Code Generator. DO NOT EDIT.
// Changes may cause incorrect behavior and will be lost if the code is regenerated.

package armnetwork

import (
	"context"
	"errors"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"net/http"
	"net/url"
	"strings"
)

// ServiceAssociationLinksClient contains the methods for the ServiceAssociationLinks group.
// Don't use this type directly, use NewServiceAssociationLinksClient() instead.
type ServiceAssociationLinksClient struct {
	internal       *arm.Client
	subscriptionID string
}

// NewServiceAssociationLinksClient creates a new instance of ServiceAssociationLinksClient with the specified values.
//   - subscriptionID - The subscription credentials which uniquely identify the Microsoft Azure subscription. The subscription
//     ID forms part of the URI for every service call.
//   - credential - used to authorize requests. Usually a credential from azidentity.
//   - options - pass nil to accept the default values.
func NewServiceAssociationLinksClient(subscriptionID string, credential azcore.TokenCredential, options *arm.ClientOptions) (*ServiceAssociationLinksClient, error) {
	cl, err := arm.NewClient(moduleName, moduleVersion, credential, options)
	if err != nil {
		return nil, err
	}
	client := &ServiceAssociationLinksClient{
		subscriptionID: subscriptionID,
		internal:       cl,
	}
	return client, nil
}

// List - Gets a list of service association links for a subnet.
// If the operation fails it returns an *azcore.ResponseError type.
//
// Generated from API version 2024-07-01
//   - resourceGroupName - The name of the resource group.
//   - virtualNetworkName - The name of the virtual network.
//   - subnetName - The name of the subnet.
//   - options - ServiceAssociationLinksClientListOptions contains the optional parameters for the ServiceAssociationLinksClient.List
//     method.
func (client *ServiceAssociationLinksClient) List(ctx context.Context, resourceGroupName string, virtualNetworkName string, subnetName string, options *ServiceAssociationLinksClientListOptions) (ServiceAssociationLinksClientListResponse, error) {
	var err error
	const operationName = "ServiceAssociationLinksClient.List"
	ctx = context.WithValue(ctx, runtime.CtxAPINameKey{}, operationName)
	ctx, endSpan := runtime.StartSpan(ctx, operationName, client.internal.Tracer(), nil)
	defer func() { endSpan(err) }()
	req, err := client.listCreateRequest(ctx, resourceGroupName, virtualNetworkName, subnetName, options)
	if err != nil {
		return ServiceAssociationLinksClientListResponse{}, err
	}
	httpResp, err := client.internal.Pipeline().Do(req)
	if err != nil {
		return ServiceAssociationLinksClientListResponse{}, err
	}
	if !runtime.HasStatusCode(httpResp, http.StatusOK) {
		err = runtime.NewResponseError(httpResp)
		return ServiceAssociationLinksClientListResponse{}, err
	}
	resp, err := client.listHandleResponse(httpResp)
	return resp, err
}

// listCreateRequest creates the List request.
func (client *ServiceAssociationLinksClient) listCreateRequest(ctx context.Context, resourceGroupName string, virtualNetworkName string, subnetName string, _ *ServiceAssociationLinksClientListOptions) (*policy.Request, error) {
	urlPath := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/virtualNetworks/{virtualNetworkName}/subnets/{subnetName}/ServiceAssociationLinks"
	if resourceGroupName == "" {
		return nil, errors.New("parameter resourceGroupName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{resourceGroupName}", url.PathEscape(resourceGroupName))
	if virtualNetworkName == "" {
		return nil, errors.New("parameter virtualNetworkName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{virtualNetworkName}", url.PathEscape(virtualNetworkName))
	if subnetName == "" {
		return nil, errors.New("parameter subnetName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{subnetName}", url.PathEscape(subnetName))
	if client.subscriptionID == "" {
		return nil, errors.New("parameter client.subscriptionID cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{subscriptionId}", url.PathEscape(client.subscriptionID))
	req, err := runtime.NewRequest(ctx, http.MethodGet, runtime.JoinPaths(client.internal.Endpoint(), urlPath))
	if err != nil {
		return nil, err
	}
	reqQP := req.Raw().URL.Query()
	reqQP.Set("api-version", "2024-07-01")
	req.Raw().URL.RawQuery = reqQP.Encode()
	req.Raw().Header["Accept"] = []string{"application/json"}
	return req, nil
}

// listHandleResponse handles the List response.
func (client *ServiceAssociationLinksClient) listHandleResponse(resp *http.Response) (ServiceAssociationLinksClientListResponse, error) {
	result := ServiceAssociationLinksClientListResponse{}
	if err := runtime.UnmarshalAsJSON(resp, &result.ServiceAssociationLinksListResult); err != nil {
		return ServiceAssociationLinksClientListResponse{}, err
	}
	return result, nil
}
