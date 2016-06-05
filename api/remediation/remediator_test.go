package remediation

import "testing"

const remediatorName = "TestR"

func createTestRemediator(config ConfigData) Remediator {
	return nil
}

func TestValidRegistration(t *testing.T) {
	if err := RegisterRemediator(remediatorName, createTestRemediator); err != nil {
		t.Errorf("Unexpected error during registration %v", err)
	}

	if _, err := GetRemediatorCreator(remediatorName); err != nil {
		t.Error("Error getting back creator")
	}
}

func TestNotRegistered(t *testing.T) {
	if _, err := GetRemediatorCreator("Invalid"); err == nil {
		t.Errorf("Expected error for invalid name")
	}
}

func TestDuplicate(t *testing.T) {
	RegisterRemediator(remediatorName, createTestRemediator)
	if err := RegisterRemediator(remediatorName, createTestRemediator); err == nil {
		t.Error("Expected error for duplicate registration")
	}
}
