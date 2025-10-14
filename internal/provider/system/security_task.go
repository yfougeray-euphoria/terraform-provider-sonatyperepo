/*
 * Copyright (c) 2019-present Sonatype, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package system provides Terraform resources for Sonatype Nexus Repository system management.
//
// Key implementation notes for securityTaskResource:
//
// 1. API Limitations:
//    - CreateTask API only returns HTTP response, not the created task object
//    - TaskXO (read) and TaskTemplateXO (write) have different field structures
//    - Some fields (AlertEmail, NotificationCondition, CronExpression) are not available in TaskXO
//    - UpdateTask API has validation conflicts with the Type field requirement
//
// 2. Workarounds:
//    - Task ID extraction tries Location header first, then searches by name/type
//    - Read operation fetches both task details and template for complete state
//    - State reconciliation handles missing fields gracefully
//    - Updates trigger recreation (destroy + create) due to API limitations
//
// 3. Recreate Behavior:
//    - Due to API constraints with the Type field during updates, this resource
//      will recreate (destroy then create) tasks when any field changes
//    - This ensures consistent behavior and avoids complex API workarounds
//    - Users should be aware that task history will be lost during updates
//
// 4. Dynamic Properties:
//    - The properties map allows flexible configuration for different task types
//    - Task-specific properties are merged into the template's Properties field
//    - Properties are preserved during read operations and state management
//
// 5. Frequency/Scheduling:
//    - Supports multiple schedule types: manual, once, hourly, daily, weekly, monthly, cron
//    - Cron expressions MUST be set in the Frequency object, NOT in Properties
//    - Weekly/Monthly schedules use RecurringDays array
//    - Backward compatibility maintained for cron_expression without schedule
//    - Nexus uses 6-field cron format: seconds minutes hours day month dayofweek

package system

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"terraform-provider-sonatyperepo/internal/provider/common"
	"terraform-provider-sonatyperepo/internal/provider/model"

	sonatyperepo "github.com/sonatype-nexus-community/nexus-repo-api-client-go/v3"
)

// Task-specific error constants
const (
	errorCreatingTask = "Error Creating Task"
	errorReadingTask  = "Error Reading Task"
	errorDeletingTask = "Error Deleting Task"
)

// securityTaskResource is the resource implementation.
type securityTaskResource struct {
	common.BaseResource
}

// NewSecurityTaskResource is a helper function to simplify the provider implementation.
func NewSecurityTaskResource() resource.Resource {
	return &securityTaskResource{}
}

// ImportState implements the resource.ResourceWithImportState interface.
func (r *securityTaskResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The import ID should be the task ID
	taskID := req.ID
	
	// Validate that the task ID is not empty
	if taskID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"Task ID cannot be empty. Please provide a valid task ID.",
		)
		return
	}

	// Set the ID in the state
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), taskID)...)
	
	// Note: The Read method will be called automatically after import to populate the rest of the state
}

// Metadata returns the resource type name.
func (r *securityTaskResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_security_task"
}

// Schema defines the schema for the resource.
func (r *securityTaskResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Sonatype Nexus Repository system tasks. This resource manages the creation and lifecycle of system tasks.\n\n" +
			"**Important Note**: Due to API limitations with task updates, this resource will **recreate** (destroy and create) " +
			"the task whenever any configuration changes. This means task execution history will be lost during updates.\n\n" +
			"## Scheduling Options\n\n" +
			"Tasks can be scheduled using different methods:\n" +
			"- **manual**: Task runs only when manually triggered\n" +
			"- **once**: Task runs once at a specified time\n" +
			"- **hourly**: Task runs every hour\n" +
			"- **daily**: Task runs once per day\n" +
			"- **weekly**: Task runs on specified days of the week (1-7, Monday-Sunday)\n" +
			"- **monthly**: Task runs on specified days of the month (1-31)\n" +
			"- **cron**: Task runs based on cron expression (6-field format: seconds minutes hours day month dayofweek)\n\n" +
			"## Common Task Types and Required Properties\n\n" +
			"- **repository.cleanup**: Requires `repositoryName` property\n" +
			"- **blobstore.compact**: Requires `blobStoreName` property\n" +
			"- **repository.rebuild-index**: Requires `repositoryName` property\n" +
			"- **security.purge-api-keys**: No additional properties required\n\n" +
			"## Import\n\n" +
			"Tasks can be imported using their task ID:\n\n" +
			"```\n" +
			"terraform import sonatyperepo_security_task.example <task-id>\n" +
			"```",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Task identifier",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Task name",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"type_id": schema.StringAttribute{
				Description: "Task type identifier",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the task is enabled",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"alert_email": schema.StringAttribute{
				Description: "Email address to send alerts to",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"notification_condition": schema.StringAttribute{
				Description: "When to send notification emails",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("SUCCESS", "FAILURE", "SUCCESS_FAILURE"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"schedule": schema.StringAttribute{
				Description: "Schedule type for the task. Valid values: manual, once, hourly, daily, weekly, monthly, cron. " +
					"If not specified but cron_expression is provided, defaults to 'cron'.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.OneOf("manual", "once", "hourly", "daily", "weekly", "monthly", "cron"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"cron_expression": schema.StringAttribute{
				Description: "Cron expression for scheduling the task. Required when schedule is 'cron'. " +
					"Uses 6-field format: seconds minutes hours day month dayofweek (e.g., '0 0 2 * * 0' for Sunday at 2 AM).",
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"recurring_days": schema.ListAttribute{
				Description: "Days when the task should run. For weekly schedule: 1-7 (Monday-Sunday), " +
					"for monthly schedule: 1-31 (day of month). Required for weekly and monthly schedules.",
				Optional:    true,
				ElementType: types.Int64Type,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"properties": schema.MapAttribute{
				Description: "Task-specific properties. The required properties depend on the task type. " +
					"Common examples: repositoryName (for repository tasks), blobStoreName (for blobstore tasks), " +
					"criteria (for cleanup tasks), etc.",
				Optional:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.Map{
					mapplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

// Create creates the resource and sets the initial Terraform state.
func (r *securityTaskResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Retrieve values from plan
	var plan model.SecurityTaskModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Error(ctx, fmt.Sprintf("Getting request data has errors: %s", resp.Diagnostics.Errors()))
		return
	}

	// Set up authentication context
	ctx = context.WithValue(
		ctx,
		sonatyperepo.ContextBasicAuth,
		r.Auth,
	)

	// First, get the task template to build the proper task structure
	templateResp, httpResp, err := r.Client.TasksAPI.GetTaskTemplate(ctx, plan.TypeID.ValueString()).Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			errorCreatingTask,
			fmt.Sprintf("Unable to get task template for type %s: %s", plan.TypeID.ValueString(), err.Error()),
		)
		return
	}
	if httpResp.StatusCode != http.StatusOK {
		resp.Diagnostics.AddError(
			errorCreatingTask,
			fmt.Sprintf("Unexpected response when getting task template: %d, status: %s", httpResp.StatusCode, httpResp.Status),
		)
		return
	}

	// Create task template from plan
	taskTemplate, buildErr := r.buildTaskTemplateFromPlan(ctx, plan, templateResp)
	if buildErr != nil {
		resp.Diagnostics.AddError(
			errorCreatingTask,
			fmt.Sprintf("Unable to build task template: %s", buildErr.Error()),
		)
		return
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating task with name: %s, type: %s", plan.Name.ValueString(), plan.TypeID.ValueString()))

	// Call API to Create
	apiResponse, err := r.Client.TasksAPI.CreateTask(ctx).Body(*taskTemplate).Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			errorCreatingTask,
			fmt.Sprintf("Unable to create task: %s", err.Error()),
		)
		return
	}
	if apiResponse.StatusCode != http.StatusCreated && apiResponse.StatusCode != http.StatusOK {
		resp.Diagnostics.AddError(
			errorCreatingTask,
			fmt.Sprintf("Unexpected response code: %d, status: %s", apiResponse.StatusCode, apiResponse.Status),
		)
		return
	}

	tflog.Info(ctx, "Successfully created security task")

	// Extract the task ID from the response location header or find the task by name
	taskID := r.extractTaskIDFromResponse(apiResponse)
	if taskID == "" {
		// If we can't get ID from response, try to find the task by name and type
		taskID, err = r.findTaskIDByNameAndType(ctx, plan.Name.ValueString(), plan.TypeID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				errorCreatingTask,
				fmt.Sprintf("Unable to determine task ID: %s", err.Error()),
			)
			return
		}
	}

	plan.ID = types.StringValue(taskID)

	// Set state
	diags := resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Read refreshes the Terraform state with the latest data.
func (r *securityTaskResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Retrieve values from state
	var state model.SecurityTaskModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		tflog.Error(ctx, fmt.Sprintf("Getting state data has errors: %s", resp.Diagnostics.Errors()))
		return
	}

	ctx = context.WithValue(ctx, sonatyperepo.ContextBasicAuth, r.Auth)

	// Read API Call
	apiResponse, httpResponse, err := r.Client.TasksAPI.GetTaskById(ctx, state.ID.ValueString()).Execute()
	if err != nil {
		r.handleReadError(ctx, resp, httpResponse, err)
		return
	}

	tflog.Debug(ctx, "Successfully read task configuration from API")

	// Check if this is an import scenario by seeing if key fields are unknown
	isImport := state.Name.IsUnknown() || state.TypeID.IsUnknown() || state.Enabled.IsUnknown()

	// Update fields we can always read reliably from TaskXO
	if apiResponse.Id != nil {
		state.ID = types.StringValue(*apiResponse.Id)
	}

	if apiResponse.Name != nil {
		state.Name = types.StringValue(*apiResponse.Name)
	}

	if apiResponse.Type != nil {
		state.TypeID = types.StringValue(*apiResponse.Type)
	}

	if isImport {
		tflog.Debug(ctx, "Detected import scenario - attempting to reconstruct full state from API")
		
		// During import, try to get as much information as possible from the task template
		if apiResponse.Type != nil {
			templateResp, httpResp, templateErr := r.Client.TasksAPI.GetTaskTemplate(ctx, *apiResponse.Type).Execute()
			if templateErr != nil {
				tflog.Warn(ctx, fmt.Sprintf("Could not retrieve task template during import: %s", templateErr.Error()))
			} else if httpResp.StatusCode != http.StatusOK {
				tflog.Warn(ctx, fmt.Sprintf("Unexpected response when getting task template during import: %d", httpResp.StatusCode))
			} else {
				tflog.Debug(ctx, "Successfully retrieved task template for import")
				// Try to populate other fields from template
				r.populateStateFromTemplate(ctx, templateResp, &state)
			}
		}

		// Set enabled based on CurrentState for imports
		if apiResponse.CurrentState != nil {
			enabled := *apiResponse.CurrentState != "DISABLED"
			state.Enabled = types.BoolValue(enabled)
			tflog.Debug(ctx, fmt.Sprintf("Import - Task state: %s, enabled: %v", *apiResponse.CurrentState, enabled))
		} else {
			state.Enabled = types.BoolValue(true) // Default
		}

		// Set reasonable defaults for import when template is not available or incomplete
		if state.AlertEmail.IsUnknown() {
			state.AlertEmail = types.StringNull()
		}
		if state.NotificationCondition.IsUnknown() {
			state.NotificationCondition = types.StringValue("FAILURE")
		}
		if state.Schedule.IsUnknown() {
			state.Schedule = types.StringValue("manual")
		}
		if state.CronExpression.IsUnknown() {
			state.CronExpression = types.StringNull()
		}
		if state.RecurringDays.IsUnknown() {
			state.RecurringDays = types.ListNull(types.Int64Type)
		}
		if state.Properties.IsUnknown() {
			state.Properties = types.MapNull(types.StringType)
		}
		tflog.Debug(ctx, "Import - Set default values for any remaining unknown fields")
	} else {
		// Normal read operation - preserve existing state for fields we can't reliably read back
		tflog.Debug(ctx, "Normal read operation - preserving existing state for configuration fields")
	}

	// Save updated state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is intentionally not implemented - this resource recreates on updates
// The schema uses RequiresReplace plan modifiers on all attributes to force recreation

// Delete deletes the resource and removes the Terraform state on success.
func (r *securityTaskResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Retrieve values from state
	var state model.SecurityTaskModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		tflog.Error(ctx, fmt.Sprintf("Getting state data has errors: %s", resp.Diagnostics.Errors()))
		return
	}

	ctx = context.WithValue(
		ctx,
		sonatyperepo.ContextBasicAuth,
		r.Auth,
	)

	tflog.Debug(ctx, fmt.Sprintf("Deleting task with ID: %s", state.ID.ValueString()))

	// Call API to Delete
	apiResponse, err := r.Client.TasksAPI.DeleteTaskById(ctx, state.ID.ValueString()).Execute()
	if err != nil {
		resp.Diagnostics.AddError(
			errorDeletingTask,
			fmt.Sprintf("Unable to delete task: %s", err.Error()),
		)
		return
	}
	if apiResponse.StatusCode != http.StatusNoContent && apiResponse.StatusCode != http.StatusOK {
		resp.Diagnostics.AddError(
			errorDeletingTask,
			fmt.Sprintf("Unexpected response code: %d, status: %s", apiResponse.StatusCode, apiResponse.Status),
		)
		return
	}

	tflog.Info(ctx, "Successfully deleted security task")
}

// Helper functions

// buildTaskTemplateFromPlan creates a TaskTemplateXO from the Terraform plan
func (r *securityTaskResource) buildTaskTemplateFromPlan(ctx context.Context, plan model.SecurityTaskModel, template *sonatyperepo.TaskTemplateXO) (*sonatyperepo.TaskTemplateXO, error) {
	taskTemplate := &sonatyperepo.TaskTemplateXO{}
	
	// Copy all fields from template as base
	if template != nil {
		*taskTemplate = *template
	}

	// Set the direct fields on TaskTemplateXO
	taskTemplate.Name = plan.Name.ValueString()
	taskTemplate.Type = plan.TypeID.ValueString()
	
	// Set enabled
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		taskTemplate.Enabled = plan.Enabled.ValueBool()
	} else {
		taskTemplate.Enabled = true
	}

	// Set optional fields if provided
	if !plan.AlertEmail.IsNull() && !plan.AlertEmail.IsUnknown() {
		email := plan.AlertEmail.ValueString()
		taskTemplate.AlertEmail = &email
	}

	if !plan.NotificationCondition.IsNull() && !plan.NotificationCondition.IsUnknown() {
		taskTemplate.NotificationCondition = plan.NotificationCondition.ValueString()
	} else if template == nil {
		// Set a default notification condition if no template provided
		taskTemplate.NotificationCondition = "FAILURE"
	}

	// Handle scheduling - different types require different Frequency configurations
	if !plan.Schedule.IsNull() && !plan.Schedule.IsUnknown() {
		schedule := plan.Schedule.ValueString()
		
		frequency := sonatyperepo.FrequencyXO{
			Schedule: schedule,
		}
		
		switch schedule {
		case "cron":
			if !plan.CronExpression.IsNull() && !plan.CronExpression.IsUnknown() {
				cronExpr := plan.CronExpression.ValueString()
				frequency.CronExpression = &cronExpr
				tflog.Debug(ctx, fmt.Sprintf("Set cron expression: %s", cronExpr))
				
				// Basic validation - Nexus expects 6 fields: seconds minutes hours day month dayofweek
				cronFields := strings.Fields(cronExpr)
				if len(cronFields) != 6 {
					tflog.Warn(ctx, fmt.Sprintf("Cron expression '%s' has %d fields, Nexus expects 6 fields (seconds minutes hours day month dayofweek)", cronExpr, len(cronFields)))
				}
			} else {
				return nil, fmt.Errorf("cron_expression is required when schedule is 'cron'")
			}
		case "weekly", "monthly":
			if !plan.RecurringDays.IsNull() && !plan.RecurringDays.IsUnknown() {
				var days []int64
				if diags := plan.RecurringDays.ElementsAs(ctx, &days, false); diags.HasError() {
					return nil, fmt.Errorf("failed to convert recurring_days: %s", diags.Errors())
				}
				
				// Convert int64 to int32 for API
				var int32Days []int32
				for _, day := range days {
					int32Days = append(int32Days, int32(day))
				}
				frequency.RecurringDays = int32Days
				tflog.Debug(ctx, fmt.Sprintf("Set %s schedule with recurring days: %v", schedule, int32Days))
			} else {
				return nil, fmt.Errorf("recurring_days is required when schedule is '%s'", schedule)
			}
		case "manual", "once", "hourly", "daily":
			// These schedules don't require additional configuration
			tflog.Debug(ctx, fmt.Sprintf("Set %s schedule", schedule))
		default:
			return nil, fmt.Errorf("unsupported schedule type: %s", schedule)
		}
		
		taskTemplate.Frequency = frequency
	} else if !plan.CronExpression.IsNull() && !plan.CronExpression.IsUnknown() {
		// Backward compatibility: if cron_expression is set but no schedule, assume cron
		cronExpr := plan.CronExpression.ValueString()
		taskTemplate.Frequency = sonatyperepo.FrequencyXO{
			Schedule:       "cron",
			CronExpression: &cronExpr,
		}
		tflog.Debug(ctx, fmt.Sprintf("Set cron expression (backward compatibility): %s", cronExpr))
	} else if template != nil {
		// Preserve the original frequency from template if no scheduling specified
		taskTemplate.Frequency = template.Frequency
		tflog.Debug(ctx, "Preserved original frequency from template")
	} else {
		// Default to manual schedule if no template and no scheduling specified
		taskTemplate.Frequency = sonatyperepo.FrequencyXO{
			Schedule: "manual",
		}
		tflog.Debug(ctx, "Set default manual schedule")
	}

	// Initialize Properties map if nil
	if taskTemplate.Properties == nil {
		props := make(map[string]string)
		taskTemplate.Properties = &props
	}

	// Handle custom properties from the properties map
	if !plan.Properties.IsNull() && !plan.Properties.IsUnknown() {
		var planProps map[string]string
		if diags := plan.Properties.ElementsAs(ctx, &planProps, false); diags.HasError() {
			return nil, fmt.Errorf("failed to convert properties map: %s", diags.Errors())
		}
		
		// Merge plan properties into the template properties
		for key, value := range planProps {
			(*taskTemplate.Properties)[key] = value
		}
		
		tflog.Debug(ctx, fmt.Sprintf("Added %d custom properties to task template", len(planProps)))
	}

	return taskTemplate, nil
}

// extractTaskIDFromResponse extracts the task ID from the API response
func (r *securityTaskResource) extractTaskIDFromResponse(resp *http.Response) string {
	// Try to get ID from Location header
	location := resp.Header.Get("Location")
	if location != "" {
		// Extract ID from location header (e.g., /v1/tasks/12345)
		parts := strings.Split(location, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}

	// If no Location header, return empty string - we'll use the fallback method
	return ""
}

// findTaskIDByNameAndType finds a task ID by searching for tasks with the given name and type
func (r *securityTaskResource) findTaskIDByNameAndType(ctx context.Context, name, taskType string) (string, error) {
	// Get all tasks of the specified type
	tasksResp, httpResp, err := r.Client.TasksAPI.GetTasks(ctx).Type_(taskType).Execute()
	if err != nil {
		return "", fmt.Errorf("failed to get tasks: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected response when getting tasks: %d, status: %s", httpResp.StatusCode, httpResp.Status)
	}

	// Search for a task with the matching name
	if tasksResp != nil && tasksResp.Items != nil {
		for _, task := range tasksResp.Items {
			if task.Name != nil && *task.Name == name && task.Type != nil && *task.Type == taskType {
				if task.Id != nil {
					return *task.Id, nil
				}
			}
		}
	}

	return "", fmt.Errorf("task with name '%s' and type '%s' not found", name, taskType)
}

// handleReadError processes errors from the GetTaskById API call
func (r *securityTaskResource) handleReadError(ctx context.Context, resp *resource.ReadResponse, httpResponse *http.Response, err error) {
	if httpResponse != nil && httpResponse.StatusCode == 404 {
		resp.State.RemoveResource(ctx)
		resp.Diagnostics.AddWarning(
			"Task not found",
			fmt.Sprintf("Task does not exist: %d: %s", httpResponse.StatusCode, httpResponse.Status),
		)
		return
	}

	errorMsg := "Unable to read task"
	if httpResponse != nil {
		errorMsg = fmt.Sprintf("%s: %s", errorMsg, httpResponse.Status)
	}
	resp.Diagnostics.AddError(
		errorReadingTask,
		fmt.Sprintf("%s: %s", errorMsg, err),
	)
}

// populateStateFromTemplate populates state from template data during import scenarios
func (r *securityTaskResource) populateStateFromTemplate(ctx context.Context, template *sonatyperepo.TaskTemplateXO, state *model.SecurityTaskModel) {
	if template == nil {
		return
	}

	// Alert email
	if template.AlertEmail != nil {
		state.AlertEmail = types.StringValue(*template.AlertEmail)
	} else {
		state.AlertEmail = types.StringNull()
	}

	// Notification condition
	state.NotificationCondition = types.StringValue(template.NotificationCondition)

	// Scheduling information from Frequency object
	state.Schedule = types.StringValue(template.Frequency.Schedule)

	switch template.Frequency.Schedule {
	case "cron":
		if template.Frequency.CronExpression != nil {
			state.CronExpression = types.StringValue(*template.Frequency.CronExpression)
		} else {
			state.CronExpression = types.StringNull()
		}
		state.RecurringDays = types.ListNull(types.Int64Type)
	case "weekly", "monthly":
		state.CronExpression = types.StringNull()
		if template.Frequency.RecurringDays != nil && len(template.Frequency.RecurringDays) > 0 {
			// Convert int32 to int64 for Terraform
			var int64Days []int64
			for _, day := range template.Frequency.RecurringDays {
				int64Days = append(int64Days, int64(day))
			}
			
			recurringDaysList, diags := types.ListValueFrom(ctx, types.Int64Type, int64Days)
			if diags.HasError() {
				tflog.Error(ctx, fmt.Sprintf("Failed to convert recurring days during import: %s", diags.Errors()))
				state.RecurringDays = types.ListNull(types.Int64Type)
			} else {
				state.RecurringDays = recurringDaysList
			}
		} else {
			state.RecurringDays = types.ListNull(types.Int64Type)
		}
	default:
		// For manual, once, hourly, daily schedules
		state.CronExpression = types.StringNull()
		state.RecurringDays = types.ListNull(types.Int64Type)
	}

	// Properties
	if template.Properties != nil && len(*template.Properties) > 0 {
		// Convert template properties to Terraform map, excluding scheduling fields
		propsMap := make(map[string]string)
		
		for key, value := range *template.Properties {
			// Skip cronExpression as it should be in Frequency object
			if key != "cronExpression" {
				propsMap[key] = value
			}
		}
		
		// Only set properties if there are any (excluding filtered ones)
		if len(propsMap) > 0 {
			propsMapValue, diags := types.MapValueFrom(ctx, types.StringType, propsMap)
			if diags.HasError() {
				tflog.Error(ctx, fmt.Sprintf("Failed to convert properties during import: %s", diags.Errors()))
				state.Properties = types.MapNull(types.StringType)
			} else {
				state.Properties = propsMapValue
			}
		} else {
			state.Properties = types.MapNull(types.StringType)
		}
	} else {
		state.Properties = types.MapNull(types.StringType)
	}

	tflog.Debug(ctx, fmt.Sprintf("Import - populated state from template: Schedule=%s, Properties=%d", 
		template.Frequency.Schedule, 
		func() int {
			if template.Properties != nil {
				return len(*template.Properties)
			}
			return 0
		}()))
}