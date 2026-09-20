package sqs

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// NewClient builds the SQS client for the emulator endpoint with the role's
// credentials (5.3): compose assigns consumer keys to the consumer role and
// publisher keys to the workers role, and both are accepted end-to-end. When
// key and secret are both set they win explicitly; otherwise the default SDK
// chain applies (ambient env, shared config).
//
// Enforcement limit: MiniStack 1.5.13 accepts even bogus credentials and
// applies no queue policy, so credential separation is topological, not a
// security boundary. The financial boundary is domain revalidation inside
// the consumer, which runs identically under any broker policy.
func NewClient(ctx context.Context, endpoint, region, accessKey, secret string) (*sqs.Client, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
		config.WithBaseEndpoint(endpoint),
	}
	if accessKey != "" && secret != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secret, "")))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return sqs.NewFromConfig(cfg), nil
}

// QueueURL resolves a queue name to its URL (robust to account prefixes).
func QueueURL(ctx context.Context, client *sqs.Client, name string) (string, error) {
	out, err := client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: &name})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.QueueUrl), nil
}
