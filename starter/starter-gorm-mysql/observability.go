/*
 * Copyright 2025 The Go-Spring Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package StarterGormMySql

import (
	"gorm.io/gorm"

	gormobserve "go-spring.org/observe-gorm"
)

// applyObservability installs the shared gorm observe plugin (trace span +
// duration/in-flight metric + access log) from the gorm bridge module, labeled
// with the MySQL db.system semantic-convention value. The plugin implementation
// is shared by every gorm starter; only this label differs.
func applyObservability(db *gorm.DB, c Config) error {
	return db.Use(gormobserve.NewPlugin("mysql", c.Observability))
}
