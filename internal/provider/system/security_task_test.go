/*
 * Copyright (c) 2019-present Sonatype, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package system_test

import (
	"fmt"
	"os"
	utils_test "terraform-provider-sonatyperepo/internal/provider/utils"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const (
	resourceTypeSecurityTask = "sonatyperepo_security_task"
	resourceNameSecurityTask = "sonatyperepo_security_task.test"
	
	// Test attribute paths
	taskIDAttr                = "id"
	taskNameAttr              = "name"
	taskTypeIDAttr            = "type_id"
	taskEnabledAttr           = "enabled"
	taskAlertEmailAttr        = "alert_email"
	taskNotificationConditionAttr = "notification_condition"
	taskCronExpressionAttr    = "cron_expression"
)

func TestAccSecurityTaskResource(t *testing.T) {
	if os.Getenv("TF_ACC_SINGLE_HIT") == "1" {
		randomString := acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
		taskName := fmt.Sprintf("test-task-%s", randomString)
		
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: utils_test.TestAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				// Create and Read testing - basic task
				{
					Config: getSecurityTaskResourceConfig(taskName, "repository.docker.gc", true, "", "", ""),
					Check: resource.ComposeAggregateTestCheckFunc(
						// Verify basic task configuration
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskNameAttr, taskName),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskTypeIDAttr, "repository.docker.gc"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskEnabledAttr, "true"),
						resource.TestCheckResourceAttrSet(resourceNameSecurityTask, taskIDAttr),
					),
				},
				// Update and Read testing - with email and notification
				{
					Config: getSecurityTaskResourceConfig(taskName, "repository.docker.gc", true, "admin@example.com", "FAILURE", ""),
					Check: resource.ComposeAggregateTestCheckFunc(
						// Verify updated task configuration
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskNameAttr, taskName),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskTypeIDAttr, "repository.docker.gc"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskEnabledAttr, "true"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskAlertEmailAttr, "admin@example.com"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskNotificationConditionAttr, "FAILURE"),
						resource.TestCheckResourceAttrSet(resourceNameSecurityTask, taskIDAttr),
					),
				},
				// Update and Read testing - with cron expression
				{
					Config: getSecurityTaskResourceConfig(taskName, "repository.docker.gc", true, "admin@example.com", "SUCCESS_FAILURE", "0 2 * * *"),
					Check: resource.ComposeAggregateTestCheckFunc(
						// Verify task with cron expression
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskNameAttr, taskName),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskTypeIDAttr, "repository.docker.gc"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskEnabledAttr, "true"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskAlertEmailAttr, "admin@example.com"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskNotificationConditionAttr, "SUCCESS_FAILURE"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskCronExpressionAttr, "0 2 * * *"),
						resource.TestCheckResourceAttrSet(resourceNameSecurityTask, taskIDAttr),
					),
				},
				// Update and Read testing - disable task
				{
					Config: getSecurityTaskResourceConfig(taskName, "repository.docker.gc", false, "admin@example.com", "SUCCESS_FAILURE", "0 2 * * *"),
					Check: resource.ComposeAggregateTestCheckFunc(
						// Verify disabled task
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskNameAttr, taskName),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskTypeIDAttr, "repository.docker.gc"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskEnabledAttr, "false"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskAlertEmailAttr, "admin@example.com"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskNotificationConditionAttr, "SUCCESS_FAILURE"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskCronExpressionAttr, "0 2 * * *"),
						resource.TestCheckResourceAttrSet(resourceNameSecurityTask, taskIDAttr),
					),
				},
				// Delete testing automatically occurs in TestCase
			},
		})
	}
}

func TestAccSecurityTaskResourceMinimalConfig(t *testing.T) {
	if os.Getenv("TF_ACC_SINGLE_HIT") == "1" {
		randomString := acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
		taskName := fmt.Sprintf("minimal-task-%s", randomString)
		
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: utils_test.TestAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				// Create and Read testing - minimal configuration
				{
					Config: getSecurityTaskMinimalResourceConfig(taskName, "repository.docker.gc"),
					Check: resource.ComposeAggregateTestCheckFunc(
						// Verify minimal task configuration
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskNameAttr, taskName),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskTypeIDAttr, "repository.docker.gc"),
						resource.TestCheckResourceAttrSet(resourceNameSecurityTask, taskIDAttr),
						// Enabled should default to true if not specified
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskEnabledAttr, "true"),
					),
				},
				// Delete testing automatically occurs in TestCase
			},
		})
	}
}

func TestAccSecurityTaskResourceDifferentTypes(t *testing.T) {
	if os.Getenv("TF_ACC_SINGLE_HIT") == "1" {
		randomString := acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
		
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: utils_test.TestAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				// Create and Read testing - cleanup task
				{
					Config: getSecurityTaskResourceConfig(fmt.Sprintf("cleanup-task-%s", randomString), "repository.cleanup", true, "ops@example.com", "FAILURE", "0 3 * * 0"),
					Check: resource.ComposeAggregateTestCheckFunc(
						// Verify cleanup task configuration
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskNameAttr, fmt.Sprintf("cleanup-task-%s", randomString)),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskTypeIDAttr, "repository.cleanup"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskEnabledAttr, "true"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskAlertEmailAttr, "ops@example.com"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskNotificationConditionAttr, "FAILURE"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskCronExpressionAttr, "0 3 * * 0"),
						resource.TestCheckResourceAttrSet(resourceNameSecurityTask, taskIDAttr),
					),
				},
				// Update to different task type - this should force replacement
				{
					Config: getSecurityTaskResourceConfig(fmt.Sprintf("cleanup-task-%s", randomString), "blobstore.compact", true, "ops@example.com", "SUCCESS", "0 4 * * 1"),
					Check: resource.ComposeAggregateTestCheckFunc(
						// Verify updated task type (should be new resource due to RequiresReplace)
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskNameAttr, fmt.Sprintf("cleanup-task-%s", randomString)),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskTypeIDAttr, "blobstore.compact"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskEnabledAttr, "true"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskAlertEmailAttr, "ops@example.com"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskNotificationConditionAttr, "SUCCESS"),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskCronExpressionAttr, "0 4 * * 1"),
						resource.TestCheckResourceAttrSet(resourceNameSecurityTask, taskIDAttr),
					),
				},
				// Delete testing automatically occurs in TestCase
			},
		})
	}
}

func TestAccSecurityTaskResourceImport(t *testing.T) {
	if os.Getenv("TF_ACC_SINGLE_HIT") == "1" {
		randomString := acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
		taskName := fmt.Sprintf("import-task-%s", randomString)
		
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: utils_test.TestAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				// Create initial task
				{
					Config: getSecurityTaskResourceConfig(taskName, "repository.docker.gc", true, "import@example.com", "FAILURE", "0 1 * * *"),
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskNameAttr, taskName),
						resource.TestCheckResourceAttr(resourceNameSecurityTask, taskTypeIDAttr, "repository.docker.gc"),
						resource.TestCheckResourceAttrSet(resourceNameSecurityTask, taskIDAttr),
					),
				},
				// Test import functionality
				{
					ResourceName:      resourceNameSecurityTask,
					ImportState:       true,
					ImportStateVerify: true,
					// Some computed fields might not match exactly during import
					ImportStateVerifyIgnore: []string{},
				},
				// Delete testing automatically occurs in TestCase
			},
		})
	}
}

func getSecurityTaskResourceConfig(name, typeID string, enabled bool, alertEmail, notificationCondition, cronExpression string) string {
	config := fmt.Sprintf(`
resource "%s" "test" {
  name    = "%s"
  type_id = "%s"
  enabled = %t`, resourceTypeSecurityTask, name, typeID, enabled)

	if alertEmail != "" {
		config += fmt.Sprintf(`
  alert_email = "%s"`, alertEmail)
	}

	if notificationCondition != "" {
		config += fmt.Sprintf(`
  notification_condition = "%s"`, notificationCondition)
	}

	if cronExpression != "" {
		config += fmt.Sprintf(`
  cron_expression = "%s"`, cronExpression)
	}

	config += `
}`

	return utils_test.ProviderConfig + config
}

func getSecurityTaskMinimalResourceConfig(name, typeID string) string {
	return fmt.Sprintf(utils_test.ProviderConfig+`
resource "%s" "test" {
  name    = "%s"
  type_id = "%s"
}
`, resourceTypeSecurityTask, name, typeID)
}