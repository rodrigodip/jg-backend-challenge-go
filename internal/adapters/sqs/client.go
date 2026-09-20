package sqs

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// NewClient builds the SQS client for the emulator endpoint with the role's
// credentials (5.3). When key and secret are both set they win explicitly;
// otherwise the default SDK chain applies (ambient env, shared config).
// Either way the consumer revalidates every domain rule: broker credential
// enforcement is transport-only and never trusted for money.
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
