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
	"strconv"
	"strings"
)

// StaticCidrsClient contains the methods for the StaticCidrs group.
// Don't use this type directly, use NewStaticCidrsClient() instead.
type StaticCidrsClient struct {
	internal       *arm.Client
	subscriptionID string
}

// NewStaticCidrsClient creates a new instance of StaticCidrsClient with the specified values.
//   - subscriptionID - The subscription credentials which uniquely identify the Microsoft Azure subscription. The subscription
//     ID forms part of the URI for every service call.
//   - credential - used to authorize requests. Usually a credential from azidentity.
//   - options - pass nil to accept the default values.
func NewStaticCidrsClient(subscriptionID string, credential azcore.TokenCredential, options *arm.ClientOptions) (*StaticCidrsClient, error) {
	cl, err := arm.NewClient(moduleName, moduleVersion, credential, options)
	if err != nil {
		return nil, err
	}
	client := &StaticCidrsClient{
		subscriptionID: subscriptionID,
		internal:       cl,
	}
	return client, nil
}

// Create - Creates/Updates the Static CIDR resource.
// If the operation fails it returns an *azcore.ResponseError type.
//
// Generated from API version 2024-07-01
//   - resourceGroupName - The name of the resource group.
//   - networkManagerName - The name of the network manager.
//   - poolName - IP Address Manager Pool resource name.
//   - staticCidrName - Static Cidr allocation name.
//   - options - StaticCidrsClientCreateOptions contains the optional parameters for the StaticCidrsClient.Create method.
func (client *StaticCidrsClient) Create(ctx context.Context, resourceGroupName string, networkManagerName string, poolName string, staticCidrName string, options *StaticCidrsClientCreateOptions) (StaticCidrsClientCreateResponse, error) {
	var err error
	const operationName = "StaticCidrsClient.Create"
	ctx = context.WithValue(ctx, runtime.CtxAPINameKey{}, operationName)
	ctx, endSpan := runtime.StartSpan(ctx, operationName, client.internal.Tracer(), nil)
	defer func() { endSpan(err) }()
	req, err := client.createCreateRequest(ctx, resourceGroupName, networkManagerName, poolName, staticCidrName, options)
	if err != nil {
		return StaticCidrsClientCreateResponse{}, err
	}
	httpResp, err := client.internal.Pipeline().Do(req)
	if err != nil {
		return StaticCidrsClientCreateResponse{}, err
	}
	if !runtime.HasStatusCode(httpResp, http.StatusOK, http.StatusCreated) {
		err = runtime.NewResponseError(httpResp)
		return StaticCidrsClientCreateResponse{}, err
	}
	resp, err := client.createHandleResponse(httpResp)
	return resp, err
}

// createCreateRequest creates the Create request.
func (client *StaticCidrsClient) createCreateRequest(ctx context.Context, resourceGroupName string, networkManagerName string, poolName string, staticCidrName string, options *StaticCidrsClientCreateOptions) (*policy.Request, error) {
	urlPath := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/networkManagers/{networkManagerName}/ipamPools/{poolName}/staticCidrs/{staticCidrName}"
	if client.subscriptionID == "" {
		return nil, errors.New("parameter client.subscriptionID cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{subscriptionId}", url.PathEscape(client.subscriptionID))
	if resourceGroupName == "" {
		return nil, errors.New("parameter resourceGroupName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{resourceGroupName}", url.PathEscape(resourceGroupName))
	if networkManagerName == "" {
		return nil, errors.New("parameter networkManagerName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{networkManagerName}", url.PathEscape(networkManagerName))
	if poolName == "" {
		return nil, errors.New("parameter poolName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{poolName}", url.PathEscape(poolName))
	if staticCidrName == "" {
		return nil, errors.New("parameter staticCidrName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{staticCidrName}", url.PathEscape(staticCidrName))
	req, err := runtime.NewRequest(ctx, http.MethodPut, runtime.JoinPaths(client.internal.Endpoint(), urlPath))
	if err != nil {
		return nil, err
	}
	reqQP := req.Raw().URL.Query()
	reqQP.Set("api-version", "2024-07-01")
	req.Raw().URL.RawQuery = reqQP.Encode()
	req.Raw().Header["Accept"] = []string{"application/json"}
	if options != nil && options.Body != nil {
		if err := runtime.MarshalAsJSON(req, *options.Body); err != nil {
			return nil, err
		}
		return req, nil
	}
	return req, nil
}

// createHandleResponse handles the Create response.
func (client *StaticCidrsClient) createHandleResponse(resp *http.Response) (StaticCidrsClientCreateResponse, error) {
	result := StaticCidrsClientCreateResponse{}
	if err := runtime.UnmarshalAsJSON(resp, &result.StaticCidr); err != nil {
		return StaticCidrsClientCreateResponse{}, err
	}
	return result, nil
}

// BeginDelete - Delete the Static CIDR resource.
// If the operation fails it returns an *azcore.ResponseError type.
//
// Generated from API version 2024-07-01
//   - resourceGroupName - The name of the resource group.
//   - networkManagerName - The name of the network manager.
//   - poolName - Pool resource name.
//   - staticCidrName - StaticCidr resource name to delete.
//   - options - StaticCidrsClientBeginDeleteOptions contains the optional parameters for the StaticCidrsClient.BeginDelete method.
func (client *StaticCidrsClient) BeginDelete(ctx context.Context, resourceGroupName string, networkManagerName string, poolName string, staticCidrName string, options *StaticCidrsClientBeginDeleteOptions) (*runtime.Poller[StaticCidrsClientDeleteResponse], error) {
	if options == nil || options.ResumeToken == "" {
		resp, err := client.deleteOperation(ctx, resourceGroupName, networkManagerName, poolName, staticCidrName, options)
		if err != nil {
			return nil, err
		}
		poller, err := runtime.NewPoller(resp, client.internal.Pipeline(), &runtime.NewPollerOptions[StaticCidrsClientDeleteResponse]{
			FinalStateVia: runtime.FinalStateViaLocation,
			Tracer:        client.internal.Tracer(),
		})
		return poller, err
	} else {
		return runtime.NewPollerFromResumeToken(options.ResumeToken, client.internal.Pipeline(), &runtime.NewPollerFromResumeTokenOptions[StaticCidrsClientDeleteResponse]{
			Tracer: client.internal.Tracer(),
		})
	}
}

// Delete - Delete the Static CIDR resource.
// If the operation fails it returns an *azcore.ResponseError type.
//
// Generated from API version 2024-07-01
func (client *StaticCidrsClient) deleteOperation(ctx context.Context, resourceGroupName string, networkManagerName string, poolName string, staticCidrName string, options *StaticCidrsClientBeginDeleteOptions) (*http.Response, error) {
	var err error
	const operationName = "StaticCidrsClient.BeginDelete"
	ctx = context.WithValue(ctx, runtime.CtxAPINameKey{}, operationName)
	ctx, endSpan := runtime.StartSpan(ctx, operationName, client.internal.Tracer(), nil)
	defer func() { endSpan(err) }()
	req, err := client.deleteCreateRequest(ctx, resourceGroupName, networkManagerName, poolName, staticCidrName, options)
	if err != nil {
		return nil, err
	}
	httpResp, err := client.internal.Pipeline().Do(req)
	if err != nil {
		return nil, err
	}
	if !runtime.HasStatusCode(httpResp, http.StatusAccepted, http.StatusNoContent) {
		err = runtime.NewResponseError(httpResp)
		return nil, err
	}
	return httpResp, nil
}

// deleteCreateRequest creates the Delete request.
func (client *StaticCidrsClient) deleteCreateRequest(ctx context.Context, resourceGroupName string, networkManagerName string, poolName string, staticCidrName string, _ *StaticCidrsClientBeginDeleteOptions) (*policy.Request, error) {
	urlPath := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/networkManagers/{networkManagerName}/ipamPools/{poolName}/staticCidrs/{staticCidrName}"
	if client.subscriptionID == "" {
		return nil, errors.New("parameter client.subscriptionID cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{subscriptionId}", url.PathEscape(client.subscriptionID))
	if resourceGroupName == "" {
		return nil, errors.New("parameter resourceGroupName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{resourceGroupName}", url.PathEscape(resourceGroupName))
	if networkManagerName == "" {
		return nil, errors.New("parameter networkManagerName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{networkManagerName}", url.PathEscape(networkManagerName))
	if poolName == "" {
		return nil, errors.New("parameter poolName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{poolName}", url.PathEscape(poolName))
	if staticCidrName == "" {
		return nil, errors.New("parameter staticCidrName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{staticCidrName}", url.PathEscape(staticCidrName))
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

// Get - Gets the specific Static CIDR resource.
// If the operation fails it returns an *azcore.ResponseError type.
//
// Generated from API version 2024-07-01
//   - resourceGroupName - The name of the resource group.
//   - networkManagerName - The name of the network manager.
//   - poolName - Pool resource name.
//   - staticCidrName - StaticCidr resource name to retrieve.
//   - options - StaticCidrsClientGetOptions contains the optional parameters for the StaticCidrsClient.Get method.
func (client *StaticCidrsClient) Get(ctx context.Context, resourceGroupName string, networkManagerName string, poolName string, staticCidrName string, options *StaticCidrsClientGetOptions) (StaticCidrsClientGetResponse, error) {
	var err error
	const operationName = "StaticCidrsClient.Get"
	ctx = context.WithValue(ctx, runtime.CtxAPINameKey{}, operationName)
	ctx, endSpan := runtime.StartSpan(ctx, operationName, client.internal.Tracer(), nil)
	defer func() { endSpan(err) }()
	req, err := client.getCreateRequest(ctx, resourceGroupName, networkManagerName, poolName, staticCidrName, options)
	if err != nil {
		return StaticCidrsClientGetResponse{}, err
	}
	httpResp, err := client.internal.Pipeline().Do(req)
	if err != nil {
		return StaticCidrsClientGetResponse{}, err
	}
	if !runtime.HasStatusCode(httpResp, http.StatusOK) {
		err = runtime.NewResponseError(httpResp)
		return StaticCidrsClientGetResponse{}, err
	}
	resp, err := client.getHandleResponse(httpResp)
	return resp, err
}

// getCreateRequest creates the Get request.
func (client *StaticCidrsClient) getCreateRequest(ctx context.Context, resourceGroupName string, networkManagerName string, poolName string, staticCidrName string, _ *StaticCidrsClientGetOptions) (*policy.Request, error) {
	urlPath := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/networkManagers/{networkManagerName}/ipamPools/{poolName}/staticCidrs/{staticCidrName}"
	if client.subscriptionID == "" {
		return nil, errors.New("parameter client.subscriptionID cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{subscriptionId}", url.PathEscape(client.subscriptionID))
	if resourceGroupName == "" {
		return nil, errors.New("parameter resourceGroupName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{resourceGroupName}", url.PathEscape(resourceGroupName))
	if networkManagerName == "" {
		return nil, errors.New("parameter networkManagerName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{networkManagerName}", url.PathEscape(networkManagerName))
	if poolName == "" {
		return nil, errors.New("parameter poolName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{poolName}", url.PathEscape(poolName))
	if staticCidrName == "" {
		return nil, errors.New("parameter staticCidrName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{staticCidrName}", url.PathEscape(staticCidrName))
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
func (client *StaticCidrsClient) getHandleResponse(resp *http.Response) (StaticCidrsClientGetResponse, error) {
	result := StaticCidrsClientGetResponse{}
	if err := runtime.UnmarshalAsJSON(resp, &result.StaticCidr); err != nil {
		return StaticCidrsClientGetResponse{}, err
	}
	return result, nil
}

// NewListPager - Gets list of Static CIDR resources at Network Manager level.
//
// Generated from API version 2024-07-01
//   - resourceGroupName - The name of the resource group.
//   - networkManagerName - The name of the network manager.
//   - poolName - Pool resource name.
//   - options - StaticCidrsClientListOptions contains the optional parameters for the StaticCidrsClient.NewListPager method.
func (client *StaticCidrsClient) NewListPager(resourceGroupName string, networkManagerName string, poolName string, options *StaticCidrsClientListOptions) *runtime.Pager[StaticCidrsClientListResponse] {
	return runtime.NewPager(runtime.PagingHandler[StaticCidrsClientListResponse]{
		More: func(page StaticCidrsClientListResponse) bool {
			return page.NextLink != nil && len(*page.NextLink) > 0
		},
		Fetcher: func(ctx context.Context, page *StaticCidrsClientListResponse) (StaticCidrsClientListResponse, error) {
			ctx = context.WithValue(ctx, runtime.CtxAPINameKey{}, "StaticCidrsClient.NewListPager")
			nextLink := ""
			if page != nil {
				nextLink = *page.NextLink
			}
			resp, err := runtime.FetcherForNextLink(ctx, client.internal.Pipeline(), nextLink, func(ctx context.Context) (*policy.Request, error) {
				return client.listCreateRequest(ctx, resourceGroupName, networkManagerName, poolName, options)
			}, nil)
			if err != nil {
				return StaticCidrsClientListResponse{}, err
			}
			return client.listHandleResponse(resp)
		},
		Tracer: client.internal.Tracer(),
	})
}

// listCreateRequest creates the List request.
func (client *StaticCidrsClient) listCreateRequest(ctx context.Context, resourceGroupName string, networkManagerName string, poolName string, options *StaticCidrsClientListOptions) (*policy.Request, error) {
	urlPath := "/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/networkManagers/{networkManagerName}/ipamPools/{poolName}/staticCidrs"
	if client.subscriptionID == "" {
		return nil, errors.New("parameter client.subscriptionID cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{subscriptionId}", url.PathEscape(client.subscriptionID))
	if resourceGroupName == "" {
		return nil, errors.New("parameter resourceGroupName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{resourceGroupName}", url.PathEscape(resourceGroupName))
	if networkManagerName == "" {
		return nil, errors.New("parameter networkManagerName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{networkManagerName}", url.PathEscape(networkManagerName))
	if poolName == "" {
		return nil, errors.New("parameter poolName cannot be empty")
	}
	urlPath = strings.ReplaceAll(urlPath, "{poolName}", url.PathEscape(poolName))
	req, err := runtime.NewRequest(ctx, http.MethodGet, runtime.JoinPaths(client.internal.Endpoint(), urlPath))
	if err != nil {
		return nil, err
	}
	reqQP := req.Raw().URL.Query()
	reqQP.Set("api-version", "2024-07-01")
	if options != nil && options.Skip != nil {
		reqQP.Set("skip", strconv.FormatInt(int64(*options.Skip), 10))
	}
	if options != nil && options.SkipToken != nil {
		reqQP.Set("skipToken", *options.SkipToken)
	}
	if options != nil && options.SortKey != nil {
		reqQP.Set("sortKey", *options.SortKey)
	}
	if options != nil && options.SortValue != nil {
		reqQP.Set("sortValue", *options.SortValue)
	}
	if options != nil && options.Top != nil {
		reqQP.Set("top", strconv.FormatInt(int64(*options.Top), 10))
	}
	req.Raw().URL.RawQuery = reqQP.Encode()
	req.Raw().Header["Accept"] = []string{"application/json"}
	return req, nil
}

// listHandleResponse handles the List response.
func (client *StaticCidrsClient) listHandleResponse(resp *http.Response) (StaticCidrsClientListResponse, error) {
	result := StaticCidrsClientListResponse{}
	if err := runtime.UnmarshalAsJSON(resp, &result.StaticCidrList); err != nil {
		return StaticCidrsClientListResponse{}, err
	}
	return result, nil
}
