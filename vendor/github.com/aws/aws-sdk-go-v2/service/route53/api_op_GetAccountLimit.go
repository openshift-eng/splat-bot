// Code generated by smithy-go-codegen DO NOT EDIT.

package route53

import (
	"context"
	"fmt"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// Gets the specified limit for the current account, for example, the maximum
// number of health checks that you can create using the account.
//
// For the default limit, see [Limits] in the Amazon Route 53 Developer Guide. To request
// a higher limit, [open a case].
//
// You can also view account limits in Amazon Web Services Trusted Advisor. Sign
// in to the Amazon Web Services Management Console and open the Trusted Advisor
// console at [https://console.aws.amazon.com/trustedadvisor/]. Then choose Service limits in the navigation pane.
//
// [Limits]: https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/DNSLimitations.html
// [https://console.aws.amazon.com/trustedadvisor/]: https://console.aws.amazon.com/trustedadvisor
// [open a case]: https://console.aws.amazon.com/support/home#/case/create?issueType=service-limit-increase&limitType=service-code-route53
func (c *Client) GetAccountLimit(ctx context.Context, params *GetAccountLimitInput, optFns ...func(*Options)) (*GetAccountLimitOutput, error) {
	if params == nil {
		params = &GetAccountLimitInput{}
	}

	result, metadata, err := c.invokeOperation(ctx, "GetAccountLimit", params, optFns, c.addOperationGetAccountLimitMiddlewares)
	if err != nil {
		return nil, err
	}

	out := result.(*GetAccountLimitOutput)
	out.ResultMetadata = metadata
	return out, nil
}

// A complex type that contains information about the request to create a hosted
// zone.
type GetAccountLimitInput struct {

	// The limit that you want to get. Valid values include the following:
	//
	//   - MAX_HEALTH_CHECKS_BY_OWNER: The maximum number of health checks that you
	//   can create using the current account.
	//
	//   - MAX_HOSTED_ZONES_BY_OWNER: The maximum number of hosted zones that you can
	//   create using the current account.
	//
	//   - MAX_REUSABLE_DELEGATION_SETS_BY_OWNER: The maximum number of reusable
	//   delegation sets that you can create using the current account.
	//
	//   - MAX_TRAFFIC_POLICIES_BY_OWNER: The maximum number of traffic policies that
	//   you can create using the current account.
	//
	//   - MAX_TRAFFIC_POLICY_INSTANCES_BY_OWNER: The maximum number of traffic policy
	//   instances that you can create using the current account. (Traffic policy
	//   instances are referred to as traffic flow policy records in the Amazon Route 53
	//   console.)
	//
	// This member is required.
	Type types.AccountLimitType

	noSmithyDocumentSerde
}

// A complex type that contains the requested limit.
type GetAccountLimitOutput struct {

	// The current number of entities that you have created of the specified type. For
	// example, if you specified MAX_HEALTH_CHECKS_BY_OWNER for the value of Type in
	// the request, the value of Count is the current number of health checks that you
	// have created using the current account.
	//
	// This member is required.
	Count int64

	// The current setting for the specified limit. For example, if you specified
	// MAX_HEALTH_CHECKS_BY_OWNER for the value of Type in the request, the value of
	// Limit is the maximum number of health checks that you can create using the
	// current account.
	//
	// This member is required.
	Limit *types.AccountLimit

	// Metadata pertaining to the operation's result.
	ResultMetadata middleware.Metadata

	noSmithyDocumentSerde
}

func (c *Client) addOperationGetAccountLimitMiddlewares(stack *middleware.Stack, options Options) (err error) {
	if err := stack.Serialize.Add(&setOperationInputMiddleware{}, middleware.After); err != nil {
		return err
	}
	err = stack.Serialize.Add(&awsRestxml_serializeOpGetAccountLimit{}, middleware.After)
	if err != nil {
		return err
	}
	err = stack.Deserialize.Add(&awsRestxml_deserializeOpGetAccountLimit{}, middleware.After)
	if err != nil {
		return err
	}
	if err := addProtocolFinalizerMiddlewares(stack, options, "GetAccountLimit"); err != nil {
		return fmt.Errorf("add protocol finalizers: %v", err)
	}

	if err = addlegacyEndpointContextSetter(stack, options); err != nil {
		return err
	}
	if err = addSetLoggerMiddleware(stack, options); err != nil {
		return err
	}
	if err = addClientRequestID(stack); err != nil {
		return err
	}
	if err = addComputeContentLength(stack); err != nil {
		return err
	}
	if err = addResolveEndpointMiddleware(stack, options); err != nil {
		return err
	}
	if err = addComputePayloadSHA256(stack); err != nil {
		return err
	}
	if err = addRetry(stack, options); err != nil {
		return err
	}
	if err = addRawResponseToMetadata(stack); err != nil {
		return err
	}
	if err = addRecordResponseTiming(stack); err != nil {
		return err
	}
	if err = addClientUserAgent(stack, options); err != nil {
		return err
	}
	if err = smithyhttp.AddErrorCloseResponseBodyMiddleware(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddCloseResponseBodyMiddleware(stack); err != nil {
		return err
	}
	if err = addSetLegacyContextSigningOptionsMiddleware(stack); err != nil {
		return err
	}
	if err = addTimeOffsetBuild(stack, c); err != nil {
		return err
	}
	if err = addUserAgentRetryMode(stack, options); err != nil {
		return err
	}
	if err = addOpGetAccountLimitValidationMiddleware(stack); err != nil {
		return err
	}
	if err = stack.Initialize.Add(newServiceMetadataMiddleware_opGetAccountLimit(options.Region), middleware.Before); err != nil {
		return err
	}
	if err = addRecursionDetection(stack); err != nil {
		return err
	}
	if err = addRequestIDRetrieverMiddleware(stack); err != nil {
		return err
	}
	if err = addResponseErrorMiddleware(stack); err != nil {
		return err
	}
	if err = addRequestResponseLogging(stack, options); err != nil {
		return err
	}
	if err = addDisableHTTPSMiddleware(stack, options); err != nil {
		return err
	}
	return nil
}

func newServiceMetadataMiddleware_opGetAccountLimit(region string) *awsmiddleware.RegisterServiceMetadata {
	return &awsmiddleware.RegisterServiceMetadata{
		Region:        region,
		ServiceID:     ServiceID,
		OperationName: "GetAccountLimit",
	}
}
