package db_test

import (
	"testing"

	"github.com/ghshhf/quantum-platform/backend/db"
)

func TestTaskModelSwitchGeneratedFieldsCompile(t *testing.T) {
	_ = db.Model{
		ThinkingEnabled: true,
		ContextLimit:    200000,
		OutputLimit:     32000,
	}
	_ = db.TaskModelSwitch{}
}
