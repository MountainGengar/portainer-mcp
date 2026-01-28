package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	apimodels "github.com/portainer/client-api-go/v2/pkg/models"
	"github.com/portainer/portainer-mcp/pkg/portainer/models"
	"github.com/portainer/portainer-mcp/pkg/portainer/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestGetStacks(t *testing.T) {
	now := time.Now().Unix()
	tests := []struct {
		name          string
		mockStacks    []*apimodels.PortainereeEdgeStack
		mockError     error
		expected      []models.Stack
		expectedError bool
	}{
		{
			name: "successful retrieval",
			mockStacks: []*apimodels.PortainereeEdgeStack{
				{
					ID:           1,
					Name:         "stack1",
					CreationDate: now,
					EdgeGroups:   []int64{1, 2},
				},
				{
					ID:           2,
					Name:         "stack2",
					CreationDate: now,
					EdgeGroups:   []int64{3},
				},
			},
			expected: []models.Stack{
				{
					ID:                  1,
					Name:                "stack1",
					CreatedAt:           time.Unix(now, 0).Format(time.RFC3339),
					EnvironmentGroupIds: []int{1, 2},
				},
				{
					ID:                  2,
					Name:                "stack2",
					CreatedAt:           time.Unix(now, 0).Format(time.RFC3339),
					EnvironmentGroupIds: []int{3},
				},
			},
		},
		{
			name:       "empty stacks",
			mockStacks: []*apimodels.PortainereeEdgeStack{},
			expected:   []models.Stack{},
		},
		{
			name:          "list error",
			mockError:     errors.New("failed to list stacks"),
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := new(MockPortainerAPI)
			mockAPI.On("ListEdgeStacks").Return(tt.mockStacks, tt.mockError)

			client := &PortainerClient{cli: mockAPI}

			stacks, err := client.GetStacks()

			if tt.expectedError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, stacks)
			mockAPI.AssertExpectations(t)
		})
	}
}

func TestGetStacksRegular(t *testing.T) {
	now := time.Now().Unix()
	regularStacks := []models.RegularStack{
		{
			ID:           10,
			Name:         "regular-stack",
			Type:         2,
			EndpointId:   3,
			CreationDate: now,
			Status:       1,
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/stacks" || r.Header.Get("X-API-Key") != "test-token" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(regularStacks)
	}))
	defer server.Close()

	client := &PortainerClient{cli: new(MockPortainerAPI), serverURL: server.URL, token: "test-token"}

	stacks, err := client.GetStacks()

	assert.NoError(t, err)
	assert.Equal(t, []models.Stack{
		{
			ID:                  10,
			Name:                "regular-stack",
			CreatedAt:           time.Unix(now, 0).Format(time.RFC3339),
			EnvironmentGroupIds: []int{},
		},
	}, stacks)
}

func TestGetStackFile(t *testing.T) {
	tests := []struct {
		name          string
		stackID       int
		mockFile      string
		mockError     error
		expected      string
		expectedError bool
	}{
		{
			name:     "successful retrieval",
			stackID:  1,
			mockFile: "version: '3'\nservices:\n  web:\n    image: nginx",
			expected: "version: '3'\nservices:\n  web:\n    image: nginx",
		},
		{
			name:          "get file error",
			stackID:       2,
			mockError:     errors.New("failed to get stack file"),
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := new(MockPortainerAPI)
			mockAPI.On("GetEdgeStackFile", int64(tt.stackID)).Return(tt.mockFile, tt.mockError)

			client := &PortainerClient{cli: mockAPI}

			file, err := client.GetStackFile(tt.stackID)

			if tt.expectedError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, file)
			mockAPI.AssertExpectations(t)
		})
	}
}

func TestGetStackFileRegular(t *testing.T) {
	stackID := 42
	stackFile := "version: '3'\nservices:\n  web:\n    image: nginx"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/stacks/42/file" || r.Header.Get("X-API-Key") != "test-token" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			StackFileContent string `json:"StackFileContent"`
		}{
			StackFileContent: stackFile,
		})
	}))
	defer server.Close()

	client := &PortainerClient{cli: new(MockPortainerAPI), serverURL: server.URL, token: "test-token"}

	file, err := client.GetStackFile(stackID)

	assert.NoError(t, err)
	assert.Equal(t, stackFile, file)
}

func TestCreateStack(t *testing.T) {
	tests := []struct {
		name                string
		stackName           string
		stackFile           string
		environmentGroupIds []int
		mockID              int64
		mockError           error
		expected            int
		expectedError       bool
	}{
		{
			name:                "successful creation",
			stackName:           "test-stack",
			stackFile:           "version: '3'\nservices:\n  web:\n    image: nginx",
			environmentGroupIds: []int{1, 2},
			mockID:              1,
			expected:            1,
		},
		{
			name:                "create error",
			stackName:           "test-stack",
			stackFile:           "version: '3'\nservices:\n  web:\n    image: nginx",
			environmentGroupIds: []int{1},
			mockError:           errors.New("failed to create stack"),
			expectedError:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := new(MockPortainerAPI)
			mockAPI.On("CreateEdgeStack", tt.stackName, tt.stackFile, utils.IntToInt64Slice(tt.environmentGroupIds)).Return(tt.mockID, tt.mockError)

			client := &PortainerClient{cli: mockAPI}

			id, err := client.CreateStack(tt.stackName, tt.stackFile, tt.environmentGroupIds)

			if tt.expectedError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, id)
			mockAPI.AssertExpectations(t)
		})
	}
}

func TestUpdateStack(t *testing.T) {
	tests := []struct {
		name                string
		stackID             int
		stackFile           string
		environmentGroupIds []int
		mockError           error
		expectedError       bool
	}{
		{
			name:                "successful update",
			stackID:             1,
			stackFile:           "version: '3'\nservices:\n  web:\n    image: nginx:latest",
			environmentGroupIds: []int{1, 2},
		},
		{
			name:                "update error",
			stackID:             2,
			stackFile:           "version: '3'\nservices:\n  web:\n    image: nginx:latest",
			environmentGroupIds: []int{1},
			mockError:           errors.New("failed to update stack"),
			expectedError:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := new(MockPortainerAPI)
			mockAPI.On("UpdateEdgeStack", int64(tt.stackID), tt.stackFile, utils.IntToInt64Slice(tt.environmentGroupIds)).Return(tt.mockError)

			client := &PortainerClient{cli: mockAPI}

			err := client.UpdateStack(tt.stackID, tt.stackFile, tt.environmentGroupIds)

			if tt.expectedError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			mockAPI.AssertExpectations(t)
		})
	}
}

func TestUpdateStackRegular(t *testing.T) {
	stackID := 42
	endpointID := 21
	stackFile := "version: '3'\nservices:\n  web:\n    image: nginx:alpine"
	expectedEnv := []models.StackEnvVar{
		{Name: "DYNU_TOKEN", Value: "token"},
		{Name: "TELEGRAM_TOKEN", Value: "telegram"},
	}
	responseEnv := []stackEnvEntryAlt{
		{Name: "DYNU_TOKEN", Value: "token"},
		{Name: "TELEGRAM_TOKEN", Value: "telegram"},
	}

	var getCalled atomic.Bool
	var updateCalled atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "test-token" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/stacks/42":
			getCalled.Store(true)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				EndpointId int                `json:"EndpointId"`
				Env        []stackEnvEntryAlt `json:"Env"`
				ID         int                `json:"Id"`
				Name       string             `json:"Name"`
				Type       int                `json:"Type"`
				Status     int                `json:"Status"`
				Creation   int64              `json:"CreationDate"`
			}{
				EndpointId: endpointID,
				Env:        responseEnv,
				ID:         stackID,
				Name:       "regular-stack",
				Type:       2,
				Status:     1,
				Creation:   time.Now().Unix(),
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/stacks/42":
			updateCalled.Store(true)
			if r.URL.Query().Get("endpointId") != strconv.Itoa(endpointID) {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			var payload struct {
				StackFileContent string               `json:"StackFileContent"`
				Prune            bool                 `json:"Prune"`
				PullImage        bool                 `json:"PullImage"`
				Env              []models.StackEnvVar `json:"Env"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if payload.StackFileContent != stackFile || payload.Prune != false || payload.PullImage != false {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if !assert.Equal(t, expectedEnv, payload.Env) {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	mockAPI := new(MockPortainerAPI)
	client := &PortainerClient{cli: mockAPI, serverURL: server.URL, token: "test-token"}

	err := client.UpdateStack(stackID, stackFile, []int{1})

	assert.NoError(t, err)
	assert.True(t, getCalled.Load())
	assert.True(t, updateCalled.Load())
	mockAPI.AssertNotCalled(t, "UpdateEdgeStack", mock.Anything, mock.Anything, mock.Anything)
}

func TestUpdateStackFallbackToEdge(t *testing.T) {
	stackID := 76
	stackFile := "version: '3'\nservices:\n  web:\n    image: nginx:latest"
	environmentGroupIds := []int{1, 2}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/stacks/76" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	mockAPI := new(MockPortainerAPI)
	mockAPI.On("UpdateEdgeStack", int64(stackID), stackFile, utils.IntToInt64Slice(environmentGroupIds)).Return(nil)

	client := &PortainerClient{cli: mockAPI, serverURL: server.URL, token: "test-token"}

	err := client.UpdateStack(stackID, stackFile, environmentGroupIds)

	assert.NoError(t, err)
	mockAPI.AssertExpectations(t)
}
