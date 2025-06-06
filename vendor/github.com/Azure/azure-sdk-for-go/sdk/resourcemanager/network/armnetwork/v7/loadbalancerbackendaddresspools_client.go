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

// LoadBalancerBackendAddressPoolsClient contains the methods for the LoadBalancerBackendAddressPools group.
// Don't use this type directly, use NewLoadBalancerBackendAddressPoolsClient() instead.
type LoadBalancerBackendAddressPoolsClient struct {
	internal       *arm.Client
	subscriptionID string
}

// NewLoadBalancerBackendAddressPoolsClient creates a new instance of LoadBalancerBackendAddressPoolsClient with the specified values.
//   - subscriptionID - The subscription credentials which uniquely identify the Microsoft Azure subscription. The subscription
//     ID forms part of the URI for every service call.
//   - credential - used to authorize requests. Usually a credential from azidentity.
//   - options - pass nil to accept the default values.
func NewLoadBalancerBackendAddressPoolsClient(subscriptionID string, credential azcore.TokenCredential, options *arm.ClientOptions) (*LoadBalancerBackendAddressPoolsClient, error) {
	cl, err := arm.NewClient(moduleName, moduleVersion, credential, options)
	if err != nil {
		return nil, err
	}
	client := &LoadBalancerBackendAddressPoolsClient{
		subscriptionID: subscriptionID,
		internal:       cl,
	}
	return client, nil
}

// BeginCreateOrUpdate - Creates or updates a load balancer backend address pool.
// If the operation fails it returns an *azcore.ResponseError type.
//
// Generated from API version 2024-07-01
//   - resourceGroupName - The name of the resource group.
//   - loadBalancerName - The name of the load balancer.
//   - backendAddressPoolName - The name of the backend address pool.
//   - parameters - Parameters supplied to the create or update load balancer backend address pool operation.
//   - options - LoadBalancerBackendAddressPoolsClientBeginCreateOrUpdateOptions contains the optional parameters for the LoadBalancerBackendAddressPoolsClient.BeginCreateOrUpdate
//     method.
func (client *LoadBalancerBackendAddressPoolsClient) BeginCreateOrUpdate(ctx context.Context, resourceGroupName string, loadBalancerName string, backendAddressPoolName string, parameters BackendAddressPool, options *LoadBalancerBackendAddressPoolsClientBeginCreateOrUpdateOptions) (*runtime.Poller[LoadBalancerBackendAddressPoolsClientCreateOrUpdateResponse], error) {
	if options == nil || options.ResumeToken == "" {
		resp, err := client.createOrUpdate(ctx, resourceGroupName, loadBalancerName, backendAddressPoolName, parameters, options)
		if err != nil {
			return nil, err
		}
		poller, err := runtime.NewPoller(resp, client.internal.Pipeline(), &runtime.NewPollerOptions[LoadBalancerBackendAddressPoolsClientCreateOrUpdateResponse]{
			FinalStateVia: runtime.FinalStateViaAzureAsyncOp,
			Tracer:        client.internal.Tracer(),
		})
		return poller, err
	} else {
		return runtime.NewPollerFromResumeToken(options.ResumeToken, client.internal.Pipeline(), &runtime.NewPollerFromResumeTokenOptions[LoadBalancerBackendAddressPoolsClientCreateOrUpdateResponse]{
			Tracer: client.internal.Tracer(),
		})
	}
}

// CreateOrUpdate - Creates or updates a load balancer backend address pool.
// If the operation fails it returns an *azcore.ResponseError type.
//
// Generated from API version 2024-07-01
func (client *LoadBalancerBackendAddressPoolsClient) createOrUpdate(ctx context.Context, resourceGroupName string, loadBalancerName string, backendAddressPoolName string, parameters BackendAddressPool, options *LoadBalancerBackendAddressPoolsClientBeginCreateOrUpdateOptions) (*http.Response, error) {
	var err error
	const operationName = "LoadBalancerBackendAddressPoolsClient.BeginCreateOrUpdate"
	ctx = context.WithValue(ctx, runtime.CtxAPINameKey{}, operationName)
	ctx, endSpan := runtime.StartSpan(ctx, operationName, client.internal.Tracer(), nil)
	defer func() { endSpan(err) }()
	req, err := client.createOrUpdateCreateRequest(ctx, resourceGroupName, loadBalancerName, backendAddressPoolName, parameters, options)
	if err != nil {
		return nil, err
	}
	httpResp, err := client.internal.Pipeline().Do(req)
	if err != nil {
		return nil, err
	}
	if !runtime.HasStatusCode(httpResp, http.StatusOK, http.StatusCreated) {
		err = runtime.NewResponseError(httpResp)
		return nil, err
	}
	return httpResp, nil
}

// createOrUpdateCreateRequest creates the CreateOrUpdate request.
func (client *LoadBalancerBackendAddressPoolsClient) createOrUpdateCreateRequest(ctx context.Context, resourceGroupName string, loadBalancerName string, backendAddressPoolName string, parameters BackendAddressPool, _ *LoadBalancerBackendAddressPoolsClientBeginCreateOrUpdateOptions) (*policy.Request, error) {
	urlPath := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/loadBalancers/{loadBalancerName}/backendAddressPools/{backendAddressPoolName}"
	if resourceGroupName == "" {
		return nil, errors.New("parameter resourceGroupName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{resourceGroupName}", url.PathEscape(resourceGroupName))
	if loadBalancerName == "" {
		return nil, errors.New("parameter loadBalancerName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{loadBalancerName}", url.PathEscape(loadBalancerName))
	if backendAddressPoolName == "" {
		return nil, errors.New("parameter backendAddressPoolName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{backendAddressPoolName}", url.PathEscape(backendAddressPoolName))
	if client.subscriptionID == "" {
		return nil, errors.New("parameter client.subscriptionID cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{subscriptionId}", url.PathEscape(client.subscriptionID))
	req, err := runtime.NewRequest(ctx, http.MethodPut, runtime.JoinPaths(client.internal.Endpoint(), urlPath))
	if err != nil {
		return nil, err
	}
	reqQP := req.Raw().URL.Query()
	reqQP.Set("api-version", "2024-07-01")
	req.Raw().URL.RawQuery = reqQP.Encode()
	req.Raw().Header["Accept"] = []string{"application/json"}
	if err := runtime.MarshalAsJSON(req, parameters); err != nil {
		return nil, err
	}
	return req, nil
}

// BeginDelete - Deletes the specified load balancer backend address pool.
// If the operation fails it returns an *azcore.ResponseError type.
//
// Generated from API version 2024-07-01
//   - resourceGroupName - The name of the resource group.
//   - loadBalancerName - The name of the load balancer.
//   - backendAddressPoolName - The name of the backend address pool.
//   - options - LoadBalancerBackendAddressPoolsClientBeginDeleteOptions contains the optional parameters for the LoadBalancerBackendAddressPoolsClient.BeginDelete
//     method.
func (client *LoadBalancerBackendAddressPoolsClient) BeginDelete(ctx context.Context, resourceGroupName string, loadBalancerName string, backendAddressPoolName string, options *LoadBalancerBackendAddressPoolsClientBeginDeleteOptions) (*runtime.Poller[LoadBalancerBackendAddressPoolsClientDeleteResponse], error) {
	if options == nil || options.ResumeToken == "" {
		resp, err := client.deleteOperation(ctx, resourceGroupName, loadBalancerName, backendAddressPoolName, options)
		if err != nil {
			return nil, err
		}
		poller, err := runtime.NewPoller(resp, client.internal.Pipeline(), &runtime.NewPollerOptions[LoadBalancerBackendAddressPoolsClientDeleteResponse]{
			FinalStateVia: runtime.FinalStateViaLocation,
			Tracer:        client.internal.Tracer(),
		})
		return poller, err
	} else {
		return runtime.NewPollerFromResumeToken(options.ResumeToken, client.internal.Pipeline(), &runtime.NewPollerFromResumeTokenOptions[LoadBalancerBackendAddressPoolsClientDeleteResponse]{
			Tracer: client.internal.Tracer(),
		})
	}
}

// Delete - Deletes the specified load balancer backend address pool.
// If the operation fails it returns an *azcore.ResponseError type.
//
// Generated from API version 2024-07-01
func (client *LoadBalancerBackendAddressPoolsClient) deleteOperation(ctx context.Context, resourceGroupName string, loadBalancerName string, backendAddressPoolName string, options *LoadBalancerBackendAddressPoolsClientBeginDeleteOptions) (*http.Response, error) {
	var err error
	const operationName = "LoadBalancerBackendAddressPoolsClient.BeginDelete"
	ctx = context.WithValue(ctx, runtime.CtxAPINameKey{}, operationName)
	ctx, endSpan := runtime.StartSpan(ctx, operationName, client.internal.Tracer(), nil)
	defer func() { endSpan(err) }()
	req, err := client.deleteCreateRequest(ctx, resourceGroupName, loadBalancerName, backendAddressPoolName, options)
	if err != nil {
		return nil, err
	}
	httpResp, err := client.internal.Pipeline().Do(req)
	if err != nil {
		return nil, err
	}
	if !runtime.HasStatusCode(httpResp, http.StatusOK, http.StatusAccepted, http.StatusNoContent) {
		err = runtime.NewResponseError(httpResp)
		return nil, err
	}
	return httpResp, nil
}

// deleteCreateRequest creates the Delete request.
func (client *LoadBalancerBackendAddressPoolsClient) deleteCreateRequest(ctx context.Context, resourceGroupName string, loadBalancerName string, backendAddressPoolName string, _ *LoadBalancerBackendAddressPoolsClientBeginDeleteOptions) (*policy.Request, error) {
	urlPath := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/loadBalancers/{loadBalancerName}/backendAddressPools/{backendAddressPoolName}"
	if resourceGroupName == "" {
		return nil, errors.New("parameter resourceGroupName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{resourceGroupName}", url.PathEscape(resourceGroupName))
	if loadBalancerName == "" {
		return nil, errors.New("parameter loadBalancerName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{loadBalancerName}", url.PathEscape(loadBalancerName))
	if backendAddressPoolName == "" {
		return nil, errors.New("parameter backendAddressPoolName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{backendAddressPoolName}", url.PathEscape(backendAddressPoolName))
	if client.subscriptionID == "" {
		return nil, errors.New("parameter client.subscriptionID cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{subscriptionId}", url.PathEscape(client.subscriptionID))
	req, err := runtime.NewRequest(ctx, http.MethodDelete, runtime.JoinPaths(client.internal.Endpoint(), urlPath))
	if err != nil {
		return nil, err
	}
	reqQP := req.Raw().URL.Query()
	reqQP.Set("api-version", "2024-07-01")
	req.Raw().URL.RawQuery = reqQP.Encode()
	req.Raw().Header["Accept"] = []string{"application/json"}
	return req, nil
}

// Get - Gets load balancer backend address pool.
// If the operation fails it returns an *azcore.ResponseError type.
//
// Generated from API version 2024-07-01
//   - resourceGroupName - The name of the resource group.
//   - loadBalancerName - The name of the load balancer.
//   - backendAddressPoolName - The name of the backend address pool.
//   - options - LoadBalancerBackendAddressPoolsClientGetOptions contains the optional parameters for the LoadBalancerBackendAddressPoolsClient.Get
//     method.
func (client *LoadBalancerBackendAddressPoolsClient) Get(ctx context.Context, resourceGroupName string, loadBalancerName string, backendAddressPoolName string, options *LoadBalancerBackendAddressPoolsClientGetOptions) (LoadBalancerBackendAddressPoolsClientGetResponse, error) {
	var err error
	const operationName = "LoadBalancerBackendAddressPoolsClient.Get"
	ctx = context.WithValue(ctx, runtime.CtxAPINameKey{}, operationName)
	ctx, endSpan := runtime.StartSpan(ctx, operationName, client.internal.Tracer(), nil)
	defer func() { endSpan(err) }()
	req, err := client.getCreateRequest(ctx, resourceGroupName, loadBalancerName, backendAddressPoolName, options)
	if err != nil {
		return LoadBalancerBackendAddressPoolsClientGetResponse{}, err
	}
	httpResp, err := client.internal.Pipeline().Do(req)
	if err != nil {
		return LoadBalancerBackendAddressPoolsClientGetResponse{}, err
	}
	if !runtime.HasStatusCode(httpResp, http.StatusOK) {
		err = runtime.NewResponseError(httpResp)
		return LoadBalancerBackendAddressPoolsClientGetResponse{}, err
	}
	resp, err := client.getHandleResponse(httpResp)
	return resp, err
}

// getCreateRequest creates the Get request.
func (client *LoadBalancerBackendAddressPoolsClient) getCreateRequest(ctx context.Context, resourceGroupName string, loadBalancerName string, backendAddressPoolName string, _ *LoadBalancerBackendAddressPoolsClientGetOptions) (*policy.Request, error) {
	urlPath := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/loadBalancers/{loadBalancerName}/backendAddressPools/{backendAddressPoolName}"
	if resourceGroupName == "" {
		return nil, errors.New("parameter resourceGroupName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{resourceGroupName}", url.PathEscape(resourceGroupName))
	if loadBalancerName == "" {
		return nil, errors.New("parameter loadBalancerName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{loadBalancerName}", url.PathEscape(loadBalancerName))
	if backendAddressPoolName == "" {
		return nil, errors.New("parameter backendAddressPoolName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{backendAddressPoolName}", url.PathEscape(backendAddressPoolName))
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

// getHandleResponse handles the Get response.
func (client *LoadBalancerBackendAddressPoolsClient) getHandleResponse(resp *http.Response) (LoadBalancerBackendAddressPoolsClientGetResponse, error) {
	result := LoadBalancerBackendAddressPoolsClientGetResponse{}
	if err := runtime.UnmarshalAsJSON(resp, &result.BackendAddressPool); err != nil {
		return LoadBalancerBackendAddressPoolsClientGetResponse{}, err
	}
	return result, nil
}

// NewListPager - Gets all the load balancer backed address pools.
//
// Generated from API version 2024-07-01
//   - resourceGroupName - The name of the resource group.
//   - loadBalancerName - The name of the load balancer.
//   - options - LoadBalancerBackendAddressPoolsClientListOptions contains the optional parameters for the LoadBalancerBackendAddressPoolsClient.NewListPager
//     method.
func (client *LoadBalancerBackendAddressPoolsClient) NewListPager(resourceGroupName string, loadBalancerName string, options *LoadBalancerBackendAddressPoolsClientListOptions) *runtime.Pager[LoadBalancerBackendAddressPoolsClientListResponse] {
	return runtime.NewPager(runtime.PagingHandler[LoadBalancerBackendAddressPoolsClientListResponse]{
		More: func(page LoadBalancerBackendAddressPoolsClientListResponse) bool {
			return page.NextLink != nil && len(*page.NextLink) > 0
		},
		Fetcher: func(ctx context.Context, page *LoadBalancerBackendAddressPoolsClientListResponse) (LoadBalancerBackendAddressPoolsClientListResponse, error) {
			ctx = context.WithValue(ctx, runtime.CtxAPINameKey{}, "LoadBalancerBackendAddressPoolsClient.NewListPager")
			nextLink := ""
			if page != nil {
				nextLink = *page.NextLink
			}
			resp, err := runtime.FetcherForNextLink(ctx, client.internal.Pipeline(), nextLink, func(ctx context.Context) (*policy.Request, error) {
				return client.listCreateRequest(ctx, resourceGroupName, loadBalancerName, options)
			}, nil)
			if err != nil {
				return LoadBalancerBackendAddressPoolsClientListResponse{}, err
			}
			return client.listHandleResponse(resp)
		},
		Tracer: client.internal.Tracer(),
	})
}

// listCreateRequest creates the List request.
func (client *LoadBalancerBackendAddressPoolsClient) listCreateRequest(ctx context.Context, resourceGroupName string, loadBalancerName string, _ *LoadBalancerBackendAddressPoolsClientListOptions) (*policy.Request, error) {
	urlPath := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/loadBalancers/{loadBalancerName}/backendAddressPools"
	if resourceGroupName == "" {
		return nil, errors.New("parameter resourceGroupName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{resourceGroupName}", url.PathEscape(resourceGroupName))
	if loadBalancerName == "" {
		return nil, errors.New("parameter loadBalancerName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{loadBalancerName}", url.PathEscape(loadBalancerName))
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
func (client *LoadBalancerBackendAddressPoolsClient) listHandleResponse(resp *http.Response) (LoadBalancerBackendAddressPoolsClientListResponse, error) {
	result := LoadBalancerBackendAddressPoolsClientListResponse{}
	if err := runtime.UnmarshalAsJSON(resp, &result.LoadBalancerBackendAddressPoolListResult); err != nil {
		return LoadBalancerBackendAddressPoolsClientListResponse{}, err
	}
	return result, nil
}
