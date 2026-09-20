#!/usr/bin/env bash
# Provisions SQS FIFO queues with redrive. Runs as a MiniStack ready.d hook
# (/docker-entrypoint-initaws.d/ready.d) and is idempotent: identical
# attributes make re-runs return the existing queue URLs.
#
# NOTE: RedrivePolicy is passed via --cli-input-json because the
# --attributes shorthand splits values on commas, which breaks the
# policy JSON. The JSON envelope is built with python3 to avoid
# shell-quoting bugs.
set -euo pipefail

ENDPOINT="${AWS_ENDPOINT_URL:-http://localhost:4566}"
REGION="${AWS_REGION:-us-east-1}"
export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-test}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-test}"
export AWS_DEFAULT_REGION="$REGION"

sqs() {
  aws --endpoint-url="$ENDPOINT" --region="$REGION" sqs "$@"
}

queue_arn() {
  sqs get-queue-attributes --queue-url "$1" --attribute-names QueueArn \
    --query 'Attributes.QueueArn' --output text
}

create_dlq() {
  sqs create-queue --queue-name "$1" \
    --attributes FifoQueue=true,ContentBasedDedup=false \
    --query 'QueueUrl' --output text
}

create_main() {
  local name="$1" dlq_arn="$2" input
  input=$(python3 -c 'import json,sys; print(json.dumps({"QueueName": sys.argv[1], "Attributes": {"FifoQueue": "true", "ContentBasedDedup": "false", "VisibilityTimeout": "30", "RedrivePolicy": json.dumps({"deadLetterTargetArn": sys.argv[2], "maxReceiveCount": "5"})}}))' "$name" "$dlq_arn")
  sqs create-queue --cli-input-json "$input" --query 'QueueUrl' --output text
}

echo "[init] creating DLQs"
TX_DLQ_URL=$(create_dlq wager-transactions-dlq.fifo)
EV_DLQ_URL=$(create_dlq wager-events-dlq.fifo)

TX_DLQ_ARN=$(queue_arn "$TX_DLQ_URL")
EV_DLQ_ARN=$(queue_arn "$EV_DLQ_URL")

echo "[init] creating main queues with redrive (maxReceiveCount=5)"
create_main wager-transactions.fifo "$TX_DLQ_ARN"
create_main wager-events.fifo "$EV_DLQ_ARN"

echo "[init] queues ready"
sqs list-queues --query 'QueueUrls' --output text
