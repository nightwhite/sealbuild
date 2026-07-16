package main

import (
	"strings"
	"testing"
)

func TestInspectWorkerAcceptsAMD64MicroarchitectureVariants(t *testing.T) {
	workerJSON := `[{
  "id": "worker0",
  "platforms": [
    {"os": "linux", "architecture": "amd64"},
    {"os": "linux", "architecture": "amd64", "variant": "v2"},
    {"os": "linux", "architecture": "amd64", "variant": "v3"}
  ]
}]`

	if err := inspectWorkerJSON(strings.NewReader(workerJSON)); err != nil {
		t.Fatalf("inspectWorkerJSON() error = %v", err)
	}
}

func TestInspectWorkerRejectsInvalidWorkerSet(t *testing.T) {
	tests := []struct {
		name       string
		workerJSON string
		wantError  string
	}{
		{
			name:       "multiple workers",
			workerJSON: `[{"id":"worker0","platforms":[{"os":"linux","architecture":"amd64"}]},{"id":"worker1","platforms":[{"os":"linux","architecture":"amd64"}]}]`,
			wantError:  "exactly one worker",
		},
		{
			name:       "missing base platform",
			workerJSON: `[{"id":"worker0","platforms":[{"os":"linux","architecture":"amd64","variant":"v3"}]}]`,
			wantError:  "base linux/amd64 platform is required",
		},
		{
			name:       "arm platform",
			workerJSON: `[{"id":"worker0","platforms":[{"os":"linux","architecture":"amd64"},{"os":"linux","architecture":"arm64"}]}]`,
			wantError:  "worker platform linux/arm64 is not allowed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := inspectWorkerJSON(strings.NewReader(test.workerJSON))
			if err == nil {
				t.Fatal("inspectWorkerJSON() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("inspectWorkerJSON() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}
