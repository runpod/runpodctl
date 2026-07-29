//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/runpod/runpodctl/internal/api"
)

// mockWorkerImage is Runpod's public mock serverless worker (~170MB, actively
// maintained). Its handler honours mock_return / mock_delay / mock_error, which
// is exactly what an invoke test needs: a deterministic result, a slow job and a
// failing job without building an image. runpod/serverless-hello-world also
// works but is 4.6GB, so it cold-starts far slower.
const mockWorkerImage = "runpod/mock-worker:latest"

// e2eInvokeDeadline bounds the whole wait for a job. A cold CPU worker spends
// ~90s pulling the image and booting before the handler runs, which is why the
// cli default (api.DefaultInvokeWait) is minutes rather than the 30s control-plane
// timeout.
const e2eInvokeDeadline = 6 * time.Minute

func TestE2E_EndpointHealth(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	endpoints, err := client.ListEndpoints(nil)
	if err != nil {
		t.Fatalf("failed to list endpoints: %v", err)
	}
	if len(endpoints) == 0 {
		t.Skip("no endpoints on this account to check health for")
	}

	invoke, err := api.NewInvokeClient()
	if err != nil {
		t.Fatalf("failed to create invoke client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	health, err := invoke.EndpointHealth(ctx, endpoints[0].ID)
	if err != nil {
		t.Fatalf("failed to get health for %s: %v", endpoints[0].ID, err)
	}
	for _, key := range []string{"jobs", "workers"} {
		if _, ok := health[key]; !ok {
			t.Errorf("health payload has no %q: %+v", key, health)
		}
	}
	t.Logf("health for %s: %+v", endpoints[0].ID, health)
}

func TestE2E_EndpointHealthNotFound(t *testing.T) {
	invoke, err := api.NewInvokeClient()
	if err != nil {
		t.Fatalf("failed to create invoke client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = invoke.EndpointHealth(ctx, "e2e-does-not-exist-1234")
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected an *api.APIError for a bogus endpoint id, got %v", err)
	}
	if apiErr.ErrorCode() != "not_found" {
		t.Errorf("code = %q, want not_found (message %q)", apiErr.ErrorCode(), apiErr.Error())
	}
}

// TestE2E_ServerlessInvokeLifecycle creates a throwaway cpu endpoint on the
// public mock worker, invokes it three ways and deletes everything it made.
// workersMin is 0, so the endpoint costs nothing while idle and the invocations
// cost a fraction of a cent.
func TestE2E_ServerlessInvokeLifecycle(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	invoke, err := api.NewInvokeClient()
	if err != nil {
		t.Fatalf("failed to create invoke client: %v", err)
	}

	suffix := time.Now().Format("20060102150405")

	template, err := client.CreateTemplate(&api.TemplateCreateRequest{
		Name:              "e2e-invoke-" + suffix,
		ImageName:         mockWorkerImage,
		IsServerless:      true,
		ContainerDiskInGb: 10,
	})
	if err != nil {
		t.Fatalf("failed to create template: %v", err)
	}
	t.Cleanup(func() {
		if err := client.DeleteTemplate(template.ID); err != nil {
			t.Errorf("failed to delete template %s: %v", template.ID, err)
			return
		}
		t.Logf("deleted template %s", template.ID)
	})

	endpoint, err := client.CreateEndpointGQL(&api.EndpointCreateGQLInput{
		Name:        "e2e-invoke-" + suffix,
		TemplateID:  template.ID,
		InstanceIDs: []string{"cpu3g-4-16"},
		WorkersMin:  0,
		WorkersMax:  1,
	})
	if err != nil {
		t.Fatalf("failed to create endpoint: %v", err)
	}
	t.Cleanup(func() {
		if err := client.DeleteEndpoint(endpoint.ID); err != nil {
			t.Errorf("failed to delete endpoint %s: %v", endpoint.ID, err)
			return
		}
		t.Logf("deleted endpoint %s", endpoint.ID)
	})
	t.Logf("created endpoint %s on %s", endpoint.ID, mockWorkerImage)

	deadline := time.Now().Add(e2eInvokeDeadline)

	t.Run("runsync", func(t *testing.T) {
		// the first invocation is a cold start: /runsync usually hands back a
		// still-running job long before the handler finishes, which is why the cli
		// keeps polling /status on this path too.
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		defer cancel()

		job, err := invoke.RunSync(ctx, endpoint.ID, json.RawMessage(`{"mock_return":"con-688 runsync"}`))
		if err != nil {
			t.Fatalf("runsync failed: %v", err)
		}
		job, err = e2eWaitForJob(t, invoke, endpoint.ID, job, deadline)
		if err != nil {
			t.Fatalf("runsync job never finished: %v", err)
		}
		if !job.Succeeded() {
			t.Fatalf("job status = %q, want COMPLETED (payload %+v)", job.Status, job.Payload)
		}
		if got := job.Payload["output"]; got != "con-688 runsync" {
			t.Errorf("output = %v, want the handler payload echoed back", got)
		}
	})

	t.Run("async", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		job, err := invoke.Run(ctx, endpoint.ID, json.RawMessage(`{"mock_return":"con-688 async"}`))
		if err != nil {
			t.Fatalf("run failed: %v", err)
		}
		if job.ID == "" {
			t.Fatal("/run returned no job id")
		}
		// what --no-wait hands back: a queued job, nothing more.
		if job.IsTerminal() {
			t.Logf("job %s was already %s on submit", job.ID, job.Status)
		}

		job, err = e2eWaitForJob(t, invoke, endpoint.ID, job, time.Now().Add(e2eInvokeDeadline))
		if err != nil {
			t.Fatalf("async job never finished: %v", err)
		}
		if !job.Succeeded() {
			t.Fatalf("job status = %q, want COMPLETED (payload %+v)", job.Status, job.Payload)
		}
	})

	t.Run("failed job", func(t *testing.T) {
		// a handler exception is a terminal, non-completed job: the cli exits
		// non-zero with code job_failed while still printing this payload.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		job, err := invoke.RunSync(ctx, endpoint.ID, json.RawMessage(`{"mock_error":true}`))
		if err != nil {
			t.Fatalf("runsync failed: %v", err)
		}
		job, err = e2eWaitForJob(t, invoke, endpoint.ID, job, time.Now().Add(e2eInvokeDeadline))
		if err != nil {
			t.Fatalf("failing job never finished: %v", err)
		}
		if job.Status != api.JobStatusFailed {
			t.Fatalf("job status = %q, want FAILED (payload %+v)", job.Status, job.Payload)
		}
		if _, ok := job.Payload["error"]; !ok {
			t.Errorf("expected the worker error in the payload, got %+v", job.Payload)
		}
		if outcome := api.NewJobFailedError(job.ID, job.Status); outcome.ErrorCode() != "job_failed" {
			t.Errorf("code = %q, want job_failed", outcome.ErrorCode())
		}
	})
}

// e2eWaitForJob polls until the job is terminal, mirroring what the cli does.
func e2eWaitForJob(t *testing.T, invoke *api.InvokeClient, endpointID string, job *api.Job, deadline time.Time) (*api.Job, error) {
	t.Helper()
	for !job.IsTerminal() {
		if time.Now().After(deadline) {
			return job, fmt.Errorf("job %s still %s at the deadline", job.ID, job.Status)
		}
		time.Sleep(2 * time.Second)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		next, err := invoke.JobStatus(ctx, endpointID, job.ID)
		cancel()
		if err != nil {
			return job, err
		}
		job = next
	}
	t.Logf("job %s finished as %s", job.ID, job.Status)
	return job, nil
}
