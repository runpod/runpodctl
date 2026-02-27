#!/usr/bin/env bash
# integration_suite.sh: Verifies installation and exercises full-feature integration matrix.
# Reports: PASSED, FAILED, or EXPECTED_FAIL (for missing privileges/secrets).

set -e

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}Starting Unified Installation & Feature Matrix Tests...${NC}"

# --- Dependencies ---
for cmd in jq curl wget tar grep sed; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "Error: Missing required command: $cmd"
        exit 1
    fi
done

# --- Helpers ---

report_status() {
    local name=$1
    local status=$2
    local msg=$3
    case "$status" in
        "PASSED") echo -e "  [${GREEN}PASSED${NC}] $name $msg" ;;
        "FAILED") echo -e "  [${RED}FAILED${NC}] $name $msg"; return 1 ;;
        "EXPECTED_FAIL") echo -e "  [${YELLOW}EXPECTED_FAIL${NC}] $name $msg" ;;
    esac
}

run_step() {
    local name=$1
    local cmd=$2
    local expect_fail_cond=$3
    local max_attempts=${4:-1} # Default to 1 attempt unless specified
    local target_output=$5 # Optional: specific file to capture output
    local retry_delay=${6:-5} # Optional: delay between retries
    local attempt=1
    local output_file=${target_output:-$(mktemp)}

    while [ $attempt -le $max_attempts ]; do
        if eval "$cmd" > "$output_file" 2>&1; then
            report_status "$name" "PASSED"
            # Only remove if it's a generic temp file, not a requested capture
            if [ -z "$target_output" ]; then rm "$output_file"; fi
            return 0
        fi

        # Check for expected failure
        if [[ -n "$expect_fail_cond" ]] && eval "$expect_fail_cond"; then
            report_status "$name" "EXPECTED_FAIL" "(Environment limitation)"
            if [ -z "$target_output" ]; then rm "$output_file"; fi
            return 0
        fi

        if [ $attempt -lt $max_attempts ]; then
            echo "  [RETRY] $name failed (Attempt $attempt/$max_attempts). Retrying in ${retry_delay}s..."
            sleep "$retry_delay"
            attempt=$((attempt + 1))
        else
            report_status "$name" "FAILED" "\nOutput:\n$(cat "$output_file")"
            if [ -z "$target_output" ]; then rm "$output_file"; fi
            return 1
        fi
    done
}

# --- Phase 1: Installation Validation ---
echo -e "\n${GREEN}>> Phase 1: Installation Validation${NC}"

if [ "$(id -u)" -eq 0 ]; then
    echo "Environment: Root"
    run_step "Root Install (/usr/local/bin)" "bash ./install.sh"
    run_step "Binary Execution (version)" "runpodctl version"
else
    # Detect if the installer actually supports non-root
    HAS_NONROOT_SUPPORT=$(grep "detect_install_dir" ./install.sh || true)
    
    mkdir -p "$HOME/.local/bin"
    export PATH="$HOME/.local/bin:$PATH"
    
    run_step "User-space Install (~/.local/bin)" "bash ./install.sh" "[[ -z \"$HAS_NONROOT_SUPPORT\" ]]"
    
    if [[ -f "$HOME/.local/bin/runpodctl" ]]; then
        run_step "Binary Execution (version)" "$HOME/.local/bin/runpodctl version"
    fi
fi

# --- Phase 2: Feature Integration Audit (RunPod API) ---
echo -e "\n${GREEN}>> Phase 2: Feature Integration Audit (RunPod API)${NC}"

if [[ -z "$RUNPOD_API_KEY" ]]; then
    report_status "API Integration" "EXPECTED_FAIL" "(RUNPOD_API_KEY missing)"
else
    # Determine binary path
    RUNPODCTL="runpodctl"
    if [ "$(id -u)" -ne 0 ] && [[ -f "$HOME/.local/bin/runpodctl" ]]; then
        RUNPODCTL="$HOME/.local/bin/runpodctl"
    fi

    # 1. Pod Management Lifecycle
    echo "Testing Pod Lifecycle (Template: bwf8egptou)..."
    POD_NAME="ci-test-pod-$(date +%s)"
    
    create_out=$(mktemp)
    if run_step "Pod Create" "$RUNPODCTL pod create --template-id bwf8egptou --compute-type CPU --name $POD_NAME" "" 3 "$create_out"; then
        POD_ID=$(jq -r '.id' "$create_out" || true)
        rm "$create_out"
        
        if [[ -n "$POD_ID" && "$POD_ID" != "null" ]]; then
             echo "Waiting for Pod API propagation (5s)..."
             sleep 5
             # Propagation Retry: Retry every 10s for up to 2 minutes (12 attempts)
             run_step "Pod List" "$RUNPODCTL pod list --output json | jq -e \"map(select(.id == \\\"$POD_ID\\\")) | length > 0\"" "" 12 "" 10
             run_step "Pod Get" "$RUNPODCTL pod get $POD_ID" "" 3
             run_step "Pod Update" "$RUNPODCTL pod update $POD_ID --name \"${POD_NAME}-updated\"" "" 3
             run_step "Pod Stop" "$RUNPODCTL pod stop $POD_ID" "" 3
             run_step "Pod Start" "$RUNPODCTL pod start $POD_ID" "" 3
             
             # Pod Data-Plane (Send/Receive)
             echo "v1.14.15-ci-test" > ci-test-file.txt
             CODE_OUT=$(mktemp)
             echo "Starting Pod Send..."
             $RUNPODCTL send ci-test-file.txt > "$CODE_OUT" 2>&1 &
             SEND_PID=$!
             
             CROC_CODE=""
             for i in {1..30}; do
                 CROC_CODE=$(grep -oE '[a-z0-9-]+-[0-9]+$' "$CODE_OUT" | head -n 1 || true)
                 if [[ -n "$CROC_CODE" ]]; then break; fi
                 sleep 1
             done
             
             if [[ -n "$CROC_CODE" ]]; then
                 echo "Captured Croc Code: $CROC_CODE"
                 run_step "Pod Receive" "$RUNPODCTL receive $CROC_CODE" "" 2
             else
                 report_status "Croc Code Extraction" "FAILED" "Could not capture generated code. Output:\n$(cat "$CODE_OUT")"
             fi
             kill $SEND_PID 2>/dev/null || true
             rm -f "$CODE_OUT" ci-test-file.txt
             
             echo "Cleaning up Pod $POD_ID..."
             $RUNPODCTL pod delete $POD_ID || true
        else
            report_status "Pod ID Extraction" "FAILED" "Could not extract valid ID"
        fi
    fi

    # 2. Serverless Lifecycle
    echo -e "\nTesting Serverless Lifecycle (Template: wvrr20un0l)..."
    EP_NAME="ci-test-ep-$(date +%s)"
    ep_out=$(mktemp)
    if run_step "Serverless Create" "$RUNPODCTL serverless create --template-id wvrr20un0l --compute-type CPU --gpu-count 0 --workers-max 1 --name '$EP_NAME'" "" 3 "$ep_out"; then
        EP_ID=$(jq -r '.id' "$ep_out" 2>/dev/null || true)
        if [[ -z "$EP_ID" || "$EP_ID" == "null" ]]; then
            echo "Error: Failed to extract Endpoint ID from create output:"
            cat "$ep_out"
            rm "$ep_out"
            exit 1
        fi
        echo "Successfully created Endpoint: $EP_ID"
        rm "$ep_out"

        if [[ -n "$EP_ID" && "$EP_ID" != "null" ]]; then
            echo "Waiting for Serverless endpoint $EP_ID to become available (Polling for up to 5m)..."
            polling_attempt=1
            max_polling=30 # 30 attempts * 10s = 5 minutes
            ready=false
            
            while [ $polling_attempt -le $max_polling ]; do
                if $RUNPODCTL serverless get $EP_ID > /dev/null 2>&1; then
                    echo "  Endpoint $EP_ID found! Proceeding..."
                    ready=true
                    break
                fi
                echo "  [Poll $polling_attempt/$max_polling] Endpoint not found yet. Sleeping 10s..."
                sleep 10
                polling_attempt=$((polling_attempt + 1))
            done

            if [ "$ready" = false ]; then
                report_status "Serverless Propagation" "FAILED" "Endpoint $EP_ID did not appear in the API within 5 minutes."
                exit 1
            fi

            run_step "Serverless Get (Final Check)" "$RUNPODCTL serverless get $EP_ID" "" 3
            # Propagation Retry: Retry every 10s for up to 2 minutes (12 attempts)
            run_step "Serverless List" "$RUNPODCTL serverless list --output json | jq -e \"map(select(.id == \\\"$EP_ID\\\")) | length > 0\"" "" 12 "" 10
            
            # Propagation Retry: Retry every 10s for up to 2 minutes (12 attempts)
            run_step "Serverless Update" "$RUNPODCTL serverless update $EP_ID --name \"${EP_NAME}-updated\"" "" 12 "" 10
            
            echo "Testing Serverless Job Submission..."
            JOB_OUT=$(mktemp)
            # Submission is usually fast if the endpoint 'Get' works
            if run_step "Serverless Send (Job Submit)" "curl -s -X POST \"https://api.runpod.ai/v2/$EP_ID/run\" -H \"Content-Type: application/json\" -H \"Authorization: Bearer $RUNPOD_API_KEY\" -d '{\"input\": {\"test\": \"data\"}}'" "" 3 "$JOB_OUT"; then
                JOB_ID=$(jq -r '.id' "$JOB_OUT")
                if [[ -n "$JOB_ID" && "$JOB_ID" != "null" ]]; then
                    run_step "Serverless Receive (Job Status)" "curl -s -X GET \"https://api.runpod.ai/v2/$EP_ID/status/$JOB_ID\" -H \"Authorization: Bearer $RUNPOD_API_KEY\" | jq -e '.status | test(\"COMPLETED|IN_PROGRESS|IN_QUEUE|PENDING\")'" "" 5
                fi
            fi
            rm -f "$JOB_OUT"

            # Propagation Retry: Retry every 10s for up to 2 minutes (12 attempts)
            run_step "Serverless Delete" "$RUNPODCTL serverless delete $EP_ID" "" 12 "" 10
        fi
    fi
fi

echo -e "\n${GREEN}Unified Matrix Tests Completed.${NC}"
