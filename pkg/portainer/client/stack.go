package client

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/portainer/portainer-mcp/pkg/portainer/models"
	"github.com/portainer/portainer-mcp/pkg/portainer/utils"
)

// GetStacks retrieves all stacks from the Portainer server.
// This function queries regular Docker stacks via the Portainer REST API.
// Falls back to edge stacks if regular stacks API fails.
//
// Returns:
//   - A slice of Stack objects
//   - An error if the operation fails
func (c *PortainerClient) GetStacks() ([]models.Stack, error) {
	regularStacks, err := c.listRegularStacksHTTP()
	if err == nil && len(regularStacks) > 0 {
		stacks := make([]models.Stack, len(regularStacks))
		for i, regularStack := range regularStacks {
			stacks[i] = models.ConvertRegularStackToStack(&regularStack)
		}
		return stacks, nil
	}

	edgeStacks, edgeErr := c.cli.ListEdgeStacks()
	if edgeErr != nil {
		if err != nil {
			return nil, fmt.Errorf("failed to list regular stacks: %w (edge stacks also failed: %v)", err, edgeErr)
		}
		return nil, fmt.Errorf("failed to list edge stacks: %w", edgeErr)
	}

	stacks := make([]models.Stack, len(edgeStacks))
	for i, es := range edgeStacks {
		stacks[i] = models.ConvertEdgeStackToStack(es)
	}

	return stacks, nil
}

func (c *PortainerClient) listRegularStacksHTTP() ([]models.RegularStack, error) {
	serverURL := c.serverURL
	if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		serverURL = "https://" + serverURL
	}
	apiURL := fmt.Sprintf("%s/api/stacks", strings.TrimSuffix(serverURL, "/"))

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-API-Key", c.token)

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: c.skipTLSVerify},
	}
	client := &http.Client{Transport: transport}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var stacks []models.RegularStack
	if err := json.Unmarshal(body, &stacks); err != nil {
		return nil, fmt.Errorf("failed to parse response json: %w", err)
	}

	return stacks, nil
}

// GetStackFile retrieves the file content of a stack from the Portainer server.
// This function queries regular Docker stacks via the Portainer REST API.
// Falls back to edge stacks if regular stacks API fails.
//
// Parameters:
//   - id: The ID of the stack to retrieve
//
// Returns:
//   - The file content of the stack (Compose file)
//   - An error if the operation fails
func (c *PortainerClient) GetStackFile(id int) (string, error) {
	file, err := c.getRegularStackFileHTTP(id)
	if err == nil && file != "" {
		return file, nil
	}

	edgeFile, edgeErr := c.cli.GetEdgeStackFile(int64(id))
	if edgeErr != nil {
		if err != nil {
			return "", fmt.Errorf("failed to get regular stack file: %w (edge stack also failed: %v)", err, edgeErr)
		}
		return "", fmt.Errorf("failed to get edge stack file: %w", edgeErr)
	}

	return edgeFile, nil
}

// GetStackEnvNames retrieves the environment variable names for a regular stack.
func (c *PortainerClient) GetStackEnvNames(id int) ([]string, error) {
	if c.serverURL == "" || c.token == "" {
		return nil, fmt.Errorf("stack env names require server url and token")
	}

	_, env, err := c.getRegularStackDetailsHTTP(id)
	if err != nil {
		if shouldFallbackToEdge(err) {
			return nil, fmt.Errorf("stack env names are not available for edge stacks")
		}
		return nil, fmt.Errorf("failed to get stack details: %w", err)
	}

	names := make([]string, 0, len(env))
	seen := map[string]bool{}
	for _, entry := range env {
		if entry.Name == "" || seen[entry.Name] {
			continue
		}
		seen[entry.Name] = true
		names = append(names, entry.Name)
	}

	return names, nil
}

func (c *PortainerClient) getRegularStackFileHTTP(id int) (string, error) {
	serverURL := c.serverURL
	if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		serverURL = "https://" + serverURL
	}
	apiURL := fmt.Sprintf("%s/api/stacks/%d/file", strings.TrimSuffix(serverURL, "/"), id)

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-API-Key", c.token)

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: c.skipTLSVerify},
	}
	client := &http.Client{Transport: transport}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("api returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var response struct {
		StackFileContent string `json:"StackFileContent"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response json: %w", err)
	}

	return response.StackFileContent, nil
}

type apiStatusError struct {
	statusCode int
	body       string
}

func (e *apiStatusError) Error() string {
	if e.body == "" {
		return fmt.Sprintf("api returned status %d", e.statusCode)
	}
	return fmt.Sprintf("api returned status %d: %s", e.statusCode, e.body)
}

type stackEnvEntryAlt struct {
	Name  string `json:"Name"`
	Value string `json:"Value"`
}

func parseStackEnv(raw json.RawMessage) ([]models.StackEnvVar, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var env []models.StackEnvVar
	if err := json.Unmarshal(raw, &env); err == nil {
		if len(env) == 0 || hasEnvValues(env) {
			return env, nil
		}
	}

	var alt []stackEnvEntryAlt
	if err := json.Unmarshal(raw, &alt); err != nil {
		return nil, fmt.Errorf("failed to parse stack env: %w", err)
	}
	if len(alt) == 0 {
		return nil, nil
	}

	converted := make([]models.StackEnvVar, len(alt))
	for i, entry := range alt {
		converted[i] = models.StackEnvVar{Name: entry.Name, Value: entry.Value}
	}

	return converted, nil
}

func hasEnvValues(env []models.StackEnvVar) bool {
	for _, entry := range env {
		if entry.Name != "" || entry.Value != "" {
			return true
		}
	}
	return false
}

func mergeEnvOverrides(existing []models.StackEnvVar, overrides []models.StackEnvVar) []models.StackEnvVar {
	if len(overrides) == 0 {
		return existing
	}

	overrideValues := map[string]string{}
	overrideOrder := make([]string, 0, len(overrides))
	for _, override := range overrides {
		if override.Name == "" {
			continue
		}
		if _, exists := overrideValues[override.Name]; !exists {
			overrideOrder = append(overrideOrder, override.Name)
		}
		overrideValues[override.Name] = override.Value
	}

	if len(overrideValues) == 0 {
		return existing
	}

	merged := make([]models.StackEnvVar, 0, len(existing)+len(overrideValues))
	seen := map[string]bool{}
	for _, current := range existing {
		if current.Name == "" {
			continue
		}
		if overrideValue, ok := overrideValues[current.Name]; ok {
			merged = append(merged, models.StackEnvVar{Name: current.Name, Value: overrideValue})
			seen[current.Name] = true
			continue
		}
		merged = append(merged, current)
		seen[current.Name] = true
	}

	for _, name := range overrideOrder {
		if seen[name] {
			continue
		}
		merged = append(merged, models.StackEnvVar{Name: name, Value: overrideValues[name]})
		seen[name] = true
	}

	return merged
}

func (c *PortainerClient) getRegularStackDetailsHTTP(id int) (int, []models.StackEnvVar, error) {
	serverURL := c.serverURL
	if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		serverURL = "https://" + serverURL
	}
	apiURL := fmt.Sprintf("%s/api/stacks/%d", strings.TrimSuffix(serverURL, "/"), id)

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-API-Key", c.token)

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: c.skipTLSVerify},
	}
	client := &http.Client{Transport: transport}

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to make http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, nil, &apiStatusError{statusCode: resp.StatusCode, body: string(body)}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var response struct {
		EndpointId int             `json:"EndpointId"`
		Env        json.RawMessage `json:"Env"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return 0, nil, fmt.Errorf("failed to parse response json: %w", err)
	}

	env, err := parseStackEnv(response.Env)
	if err != nil {
		return 0, nil, err
	}

	return response.EndpointId, env, nil
}

func (c *PortainerClient) updateRegularStackHTTP(id int, endpointId int, file string, env []models.StackEnvVar) error {
	serverURL := c.serverURL
	if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
		serverURL = "https://" + serverURL
	}
	apiURL := fmt.Sprintf("%s/api/stacks/%d?endpointId=%d", strings.TrimSuffix(serverURL, "/"), id, endpointId)

	payload := struct {
		StackFileContent string               `json:"StackFileContent"`
		Prune            bool                 `json:"Prune"`
		PullImage        bool                 `json:"PullImage"`
		Env              []models.StackEnvVar `json:"Env"`
	}{
		StackFileContent: file,
		Prune:            false,
		PullImage:        false,
		Env:              env,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal stack update request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-API-Key", c.token)
	req.Header.Set("Content-Type", "application/json")

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: c.skipTLSVerify},
	}
	client := &http.Client{Transport: transport}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return &apiStatusError{statusCode: resp.StatusCode, body: string(body)}
	}

	return nil
}

func shouldFallbackToEdge(err error) bool {
	var apiErr *apiStatusError
	if errors.As(err, &apiErr) {
		if apiErr.statusCode == http.StatusNotFound {
			return true
		}
		if isEdgeStackErrorMessage(apiErr.body) {
			return true
		}
	}

	return isEdgeStackErrorMessage(err.Error())
}

func isEdgeStackErrorMessage(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "edgestackupdate") || strings.Contains(lower, "edge stack")
}

// CreateStack creates a new stack on the Portainer server.
// This function specifically creates a Docker Compose stack.
// Stacks are the equivalent of Edge Stacks in Portainer.
//
// Parameters:
//   - name: The name of the stack
//   - file: The file content of the stack (Compose file)
//   - environmentGroupIds: A slice of environment group IDs to include in the stack
//
// Returns:
//   - The ID of the created stack
//   - An error if the operation fails
func (c *PortainerClient) CreateStack(name, file string, environmentGroupIds []int) (int, error) {
	id, err := c.cli.CreateEdgeStack(name, file, utils.IntToInt64Slice(environmentGroupIds))
	if err != nil {
		return 0, fmt.Errorf("failed to create edge stack: %w", err)
	}

	return int(id), nil
}

// UpdateStack updates an existing stack on the Portainer server.
// This function attempts a regular stack update first and falls back to edge stacks when appropriate.
//
// Parameters:
//   - id: The ID of the stack to update
//   - file: The file content of the stack (Compose file)
//   - environmentGroupIds: A slice of environment group IDs to include in the stack
//
// Returns:
//   - An error if the operation fails
func (c *PortainerClient) UpdateStack(id int, file string, environmentGroupIds []int, envOverrides []models.StackEnvVar) error {
	if c.serverURL != "" && c.token != "" {
		endpointId, env, err := c.getRegularStackDetailsHTTP(id)
		if err == nil {
			mergedEnv := mergeEnvOverrides(env, envOverrides)
			err = c.updateRegularStackHTTP(id, endpointId, file, mergedEnv)
			if err == nil {
				return nil
			}
			if shouldFallbackToEdge(err) {
				if len(envOverrides) > 0 {
					return fmt.Errorf("stack env overrides are not supported for edge stacks")
				}
				edgeErr := c.cli.UpdateEdgeStack(int64(id), file, utils.IntToInt64Slice(environmentGroupIds))
				if edgeErr != nil {
					return fmt.Errorf("failed to update regular stack: %w (edge stack also failed: %v)", err, edgeErr)
				}
				return nil
			}
			return fmt.Errorf("failed to update regular stack: %w", err)
		}
		if shouldFallbackToEdge(err) {
			if len(envOverrides) > 0 {
				return fmt.Errorf("stack env overrides are not supported for edge stacks")
			}
			edgeErr := c.cli.UpdateEdgeStack(int64(id), file, utils.IntToInt64Slice(environmentGroupIds))
			if edgeErr != nil {
				return fmt.Errorf("failed to get regular stack details: %w (edge stack also failed: %v)", err, edgeErr)
			}
			return nil
		}
		return fmt.Errorf("failed to get regular stack details: %w", err)
	}

	if len(envOverrides) > 0 {
		return fmt.Errorf("stack env overrides require a Portainer server url and token")
	}

	err := c.cli.UpdateEdgeStack(int64(id), file, utils.IntToInt64Slice(environmentGroupIds))
	if err != nil {
		return fmt.Errorf("failed to update edge stack: %w", err)
	}

	return nil
}
