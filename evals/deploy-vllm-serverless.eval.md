# Deploy vLLM as a serverless endpoint

## Prompt

I want to run vLLM as a serverless endpoint on Runpod.

## Expected behavior

The agent should:

1. Search the hub for vLLM: `runpodctl hub search vllm`
2. Identify the official `runpod-workers/worker-vllm` listing and note its hub ID
3. Optionally get details: `runpodctl hub get <hub-id>` or `runpodctl hub get runpod-workers/worker-vllm`
4. Create a serverless endpoint using the hub ID: `runpodctl serverless create --hub-id <hub-id> --name "<name>"`
5. Verify the endpoint was created successfully
6. Clean up the endpoint after verification: `runpodctl serverless delete <endpoint-id>`

## Assertions

- Agent uses `runpodctl hub search` or `runpodctl hub list` to discover the vLLM listing
- Agent uses `runpodctl serverless create --hub-id` (not `--template-id`) to deploy
- Agent cleans up the created endpoint with `runpodctl serverless delete`
- Agent does NOT try to create a template manually
