package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
)

// ---------- Data Model ----------

type Credential struct {
	CredID        string `json:"credID"`
	StudentID     string `json:"studentID"`
	StudentName   string `json:"studentName"`
	University    string `json:"university"`
	Degree        string `json:"degree"`
	GPA           string `json:"gpa"`
	IssueDate     string `json:"issueDate"`
	Hash          string `json:"hash"` // auto-computed, SHA-256 hex
	Status        string `json:"status"` // issued / revoked
	OwnerMSP      string `json:"ownerMSP"`
	SharedWithMSP string `json:"sharedWithMSP"` // always present, may be ""
}

type IntegrityReport struct {
	CredID        string `json:"credID"`
	StoredHash    string `json:"storedHash"`
	ComputedHash  string `json:"computedHash"`
	IsHashValid   bool   `json:"isHashValid"`
	SharedWithMSP string `json:"sharedWithMSP"`
	Status        string `json:"status"`
}

// ---------- Smart Contract ----------

type SmartContract struct {
	contractapi.Contract
}

// canonical string over which the hash is computed (order is fixed)
func canonicalString(c *Credential) string {
	// Do NOT include Hash itself or mutable fields like OwnerMSP/SharedWithMSP/Status in the hash
	// Order: credID|studentID|studentName|university|degree|gpa|issueDate
	return c.CredID + "|" + c.StudentID + "|" + c.StudentName + "|" +
		c.University + "|" + c.Degree + "|" + c.GPA + "|" + c.IssueDate
}

func computeSHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ---------- Issue (Org1 writes to Org1 PDC) ----------

func (s *SmartContract) IssueCredential(ctx contractapi.TransactionContextInterface,
	credID, studentID, studentName, university, degree, gpa, issueDate, _ string) error {

	exists, err := s.CredentialExists(ctx, credID)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("credential %s already exists", credID)
	}

	mspid, err := ctx.GetClientIdentity().GetMSPID()
	if err != nil {
		return fmt.Errorf("cannot get MSP ID: %v", err)
	}

	cred := Credential{
		CredID:        credID,
		StudentID:     studentID,
		StudentName:   studentName,
		University:    university,
		Degree:        degree,
		GPA:           gpa,
		IssueDate:     issueDate,
		Status:        "issued",
		OwnerMSP:      mspid,
		SharedWithMSP: "",
	}
	// Auto-compute hash
	cred.Hash = computeSHA256Hex(canonicalString(&cred))

	data, _ := json.Marshal(cred)
	return ctx.GetStub().PutPrivateData("Org1PrivateCollection", credID, data)
}

// ---------- Read (Org1 only) ----------

func (s *SmartContract) ReadCredential(ctx contractapi.TransactionContextInterface, credID string) (*Credential, error) {
	data, err := ctx.GetStub().GetPrivateData("Org1PrivateCollection", credID)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("credential %s not found", credID)
	}

	var cred Credential
	if err := json.Unmarshal(data, &cred); err != nil {
		return nil, err
	}
	// keep field always present
	if cred.SharedWithMSP == "" {
		cred.SharedWithMSP = ""
	}
	return &cred, nil
}

// ---------- Store for Org2 (Org2 identity; verifies hash) ----------

func (s *SmartContract) StoreCredentialForOrg2(ctx contractapi.TransactionContextInterface, credJSON string) error {
	mspid, _ := ctx.GetClientIdentity().GetMSPID()
	if mspid != "Org2MSP" {
		return fmt.Errorf("only Org2 can write into Org2PrivateCollection")
	}

	var cred Credential
	if err := json.Unmarshal([]byte(credJSON), &cred); err != nil {
		return fmt.Errorf("invalid credential json: %v", err)
	}
	if cred.CredID == "" {
		return fmt.Errorf("credID required in credential json")
	}

	// Recompute hash and enforce integrity
	computed := computeSHA256Hex(canonicalString(&cred))
	if cred.Hash == "" || cred.Hash != computed {
		return fmt.Errorf("hash mismatch for credID %s: provided='%s' computed='%s'", cred.CredID, cred.Hash, computed)
	}

	cred.SharedWithMSP = "Org2MSP"
	data, _ := json.Marshal(cred)
	return ctx.GetStub().PutPrivateData("Org2PrivateCollection", cred.CredID, data)
}

// ---------- Verify (Org2 read) ----------

func (s *SmartContract) VerifyCredential(ctx contractapi.TransactionContextInterface, credID string) (*Credential, error) {
	mspid, _ := ctx.GetClientIdentity().GetMSPID()
	if mspid != "Org2MSP" {
		return nil, fmt.Errorf("only Org2 can verify credentials")
	}

	data, err := ctx.GetStub().GetPrivateData("Org2PrivateCollection", credID)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("credential %s not found in Org2 collection", credID)
	}

	var cred Credential
	if err := json.Unmarshal(data, &cred); err != nil {
		return nil, err
	}
	if cred.SharedWithMSP == "" {
		cred.SharedWithMSP = "Org2MSP"
	}
	return &cred, nil
}

// ---------- Verify hash integrity (Org2) ----------

func (s *SmartContract) VerifyCredentialIntegrity(ctx contractapi.TransactionContextInterface, credID string) (*IntegrityReport, error) {
	mspid, _ := ctx.GetClientIdentity().GetMSPID()
	if mspid != "Org2MSP" {
		return nil, fmt.Errorf("only Org2 can verify integrity")
	}

	data, err := ctx.GetStub().GetPrivateData("Org2PrivateCollection", credID)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("credential %s not found in Org2 collection", credID)
	}

	var cred Credential
	if err := json.Unmarshal(data, &cred); err != nil {
		return nil, err
	}
	computed := computeSHA256Hex(canonicalString(&cred))
	report := &IntegrityReport{
		CredID:        cred.CredID,
		StoredHash:    cred.Hash,
		ComputedHash:  computed,
		IsHashValid:   cred.Hash == computed,
		SharedWithMSP: cred.SharedWithMSP,
		Status:        cred.Status,
	}
	return report, nil
}

// ---------- Revoke (Org1 only) ----------

func (s *SmartContract) RevokeCredential(ctx contractapi.TransactionContextInterface, credID string) error {
	mspid, _ := ctx.GetClientIdentity().GetMSPID()
	if mspid != "Org1MSP" {
		return fmt.Errorf("only Org1 can revoke credentials")
	}

	data, err := ctx.GetStub().GetPrivateData("Org1PrivateCollection", credID)
	if err != nil {
		return err
	}
	if data == nil {
		return fmt.Errorf("credential %s not found", credID)
	}

	var cred Credential
	if err := json.Unmarshal(data, &cred); err != nil {
		return err
	}
	cred.Status = "revoked"
	if cred.SharedWithMSP == "" {
		cred.SharedWithMSP = ""
	}
	// Hash stays the same since canonical fields (except Status) are unchanged.
	newData, _ := json.Marshal(cred)
	return ctx.GetStub().PutPrivateData("Org1PrivateCollection", credID, newData)
}

// ---------- Exists Helper ----------

func (s *SmartContract) CredentialExists(ctx contractapi.TransactionContextInterface, credID string) (bool, error) {
	data, err := ctx.GetStub().GetPrivateData("Org1PrivateCollection", credID)
	if err != nil {
		return false, err
	}
	return data != nil, nil
}

// ---------- Main ----------

func main() {
	cc, err := contractapi.NewChaincode(new(SmartContract))
	if err != nil {
		panic(fmt.Sprintf("error creating chaincode: %v", err))
	}
	if err := cc.Start(); err != nil {
		panic(fmt.Sprintf("error starting chaincode: %v", err))
	}
}
