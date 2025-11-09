package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
)

const (
	org1PDC = "Org1PrivateCollection"
	org2PDC = "Org2PrivateCollection"
)

// ==============================
//           Models
// ==============================

type Credential struct {
	CredID        string `json:"credID"`
	StudentID     string `json:"studentID"`
	StudentName   string `json:"studentName"`
	University    string `json:"university"`
	Degree        string `json:"degree"`
	GPA           string `json:"gpa"`
	IssueDate     string `json:"issueDate"`
	Hash          string `json:"hash"`          // auto-computed, SHA-256(hex)
	Status        string `json:"status"`        // issued | revoked
	OwnerMSP      string `json:"ownerMSP"`
	SharedWithMSP string `json:"sharedWithMSP"` // required, may be ""
}

type IntegrityReport struct {
	CredID        string `json:"credID"`
	StoredHash    string `json:"storedHash"`
	ComputedHash  string `json:"computedHash"`
	IsHashValid   bool   `json:"isHashValid"`
	SharedWithMSP string `json:"sharedWithMSP"`
	Status        string `json:"status"`
}

type AuditEvent struct {
	TxID      string `json:"txID"`
	Action    string `json:"action"`   // ISSUE | SHARE_TO_ORG2 | REVOKE
	MSPID     string `json:"mspID"`
	Timestamp string `json:"timestamp"` // RFC3339
	Note      string `json:"note"`      // REQUIRED (always present; empty string is fine)
}

// ==============================
//        Smart Contract
// ==============================

type SmartContract struct {
	contractapi.Contract
}

// ==============================
//          Utilities
// ==============================

func canonicalString(c *Credential) string {
	// Hash excludes mutable/derived fields (Hash, Status, OwnerMSP, SharedWithMSP).
	return c.CredID + "|" + c.StudentID + "|" + c.StudentName + "|" +
		c.University + "|" + c.Degree + "|" + c.GPA + "|" + c.IssueDate
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (s *SmartContract) putPDC(ctx contractapi.TransactionContextInterface, collection, key string, val []byte) error {
	if err := ctx.GetStub().PutPrivateData(collection, key, val); err != nil {
		return fmt.Errorf("put private data (%s/%s): %w", collection, key, err)
	}
	return nil
}

func (s *SmartContract) getPDC(ctx contractapi.TransactionContextInterface, collection, key string) ([]byte, error) {
	val, err := ctx.GetStub().GetPrivateData(collection, key)
	if err != nil {
		return nil, fmt.Errorf("get private data (%s/%s): %w", collection, key, err)
	}
	return val, nil
}

func (s *SmartContract) putAudit(ctx contractapi.TransactionContextInterface, credID, action, note string) error {
	txID := ctx.GetStub().GetTxID()
	ts, _ := ctx.GetStub().GetTxTimestamp()
	t := time.Unix(ts.GetSeconds(), int64(ts.GetNanos())).UTC().Format(time.RFC3339)

	msp, _ := ctx.GetClientIdentity().GetMSPID()
	ev := AuditEvent{
		TxID:      txID,
		Action:    action,
		MSPID:     msp,
		Timestamp: t,
		Note:      note, // always present (can be "")
	}
	b, _ := json.Marshal(ev)

	key, err := ctx.GetStub().CreateCompositeKey("evt", []string{credID, txID})
	if err != nil {
		return fmt.Errorf("create composite key: %w", err)
	}
	if err := ctx.GetStub().PutState(key, b); err != nil {
		return fmt.Errorf("put state (audit): %w", err)
	}
	return nil
}

// ==============================
//            Queries
// ==============================

// ListHistory returns audit events for a credID from public state.
// Always returns a JSON array (never null) to satisfy Gateway schema.
func (s *SmartContract) ListHistory(ctx contractapi.TransactionContextInterface, credID string) ([]*AuditEvent, error) {
	iter, err := ctx.GetStub().GetStateByPartialCompositeKey("evt", []string{credID})
	if err != nil {
		return nil, fmt.Errorf("history query: %w", err)
	}
	defer iter.Close()

	out := make([]*AuditEvent, 0)
	for iter.HasNext() {
		kv, e := iter.Next()
		if e != nil {
			return nil, fmt.Errorf("history iterate: %w", e)
		}
		var ev AuditEvent
		if err := json.Unmarshal(kv.Value, &ev); err == nil {
			// note is required by schema; ensure presence even for very old rows
			if ev.Note == "" {
				ev.Note = ""
			}
			out = append(out, &ev)
		}
	}
	return out, nil
}

// ReadCredential returns Org1’s private record.
func (s *SmartContract) ReadCredential(ctx contractapi.TransactionContextInterface, credID string) (*Credential, error) {
	raw, err := s.getPDC(ctx, org1PDC, credID)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("credential %s not found", credID)
	}
	var cred Credential
	if err := json.Unmarshal(raw, &cred); err != nil {
		return nil, fmt.Errorf("unmarshal credential: %w", err)
	}
	// keep required field present
	if cred.SharedWithMSP == "" {
		cred.SharedWithMSP = ""
	}
	return &cred, nil
}

// VerifyCredential returns Org2’s private record (employer read).
func (s *SmartContract) VerifyCredential(ctx contractapi.TransactionContextInterface, credID string) (*Credential, error) {
	msp, _ := ctx.GetClientIdentity().GetMSPID()
	if msp != "Org2MSP" {
		return nil, fmt.Errorf("only Org2 can verify credentials")
	}
	raw, err := s.getPDC(ctx, org2PDC, credID)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("credential %s not found in Org2 collection", credID)
	}
	var cred Credential
	if err := json.Unmarshal(raw, &cred); err != nil {
		return nil, fmt.Errorf("unmarshal credential: %w", err)
	}
	if cred.SharedWithMSP == "" {
		cred.SharedWithMSP = "Org2MSP"
	}
	return &cred, nil
}

// VerifyCredentialIntegrity recomputes the canonical hash for Org2’s view.
func (s *SmartContract) VerifyCredentialIntegrity(ctx contractapi.TransactionContextInterface, credID string) (*IntegrityReport, error) {
	msp, _ := ctx.GetClientIdentity().GetMSPID()
	if msp != "Org2MSP" {
		return nil, fmt.Errorf("only Org2 can verify integrity")
	}
	raw, err := s.getPDC(ctx, org2PDC, credID)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("credential %s not found in Org2 collection", credID)
	}
	var cred Credential
	if err := json.Unmarshal(raw, &cred); err != nil {
		return nil, fmt.Errorf("unmarshal credential: %w", err)
	}
	computed := sha256Hex(canonicalString(&cred))
	return &IntegrityReport{
		CredID:        cred.CredID,
		StoredHash:    cred.Hash,
		ComputedHash:  computed,
		IsHashValid:   cred.Hash == computed,
		SharedWithMSP: cred.SharedWithMSP,
		Status:        cred.Status,
	}, nil
}

// ==============================
//        Transactions
// ==============================

// IssueCredential creates Org1’s private record with an auto-computed hash.
func (s *SmartContract) IssueCredential(ctx contractapi.TransactionContextInterface,
	credID, studentID, studentName, university, degree, gpa, issueDate, _ string) error {

	if credID == "" {
		return fmt.Errorf("credID is required")
	}
	exists, err := s.CredentialExists(ctx, credID)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("credential %s already exists", credID)
	}

	msp, err := ctx.GetClientIdentity().GetMSPID()
	if err != nil {
		return fmt.Errorf("get MSP ID: %w", err)
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
		OwnerMSP:      msp,
		SharedWithMSP: "",
	}
	cred.Hash = sha256Hex(canonicalString(&cred))

	b, _ := json.Marshal(cred)
	if err := s.putPDC(ctx, org1PDC, credID, b); err != nil {
		return err
	}
	return s.putAudit(ctx, credID, "ISSUE", "")
}

// StoreCredentialForOrg2 upserts Org2’s private copy (Org2 identity required).
// It enforces that the provided hash matches the recomputed canonical hash.
func (s *SmartContract) StoreCredentialForOrg2(ctx contractapi.TransactionContextInterface, credJSON string) error {
	msp, _ := ctx.GetClientIdentity().GetMSPID()
	if msp != "Org2MSP" {
		return fmt.Errorf("only Org2 can write into %s", org2PDC)
	}
	var cred Credential
	if err := json.Unmarshal([]byte(credJSON), &cred); err != nil {
		return fmt.Errorf("invalid credential json: %w", err)
	}
	if cred.CredID == "" {
		return fmt.Errorf("credID required in credential json")
	}

	computed := sha256Hex(canonicalString(&cred))
	if cred.Hash == "" || cred.Hash != computed {
		return fmt.Errorf("hash mismatch for %s: provided='%s' computed='%s'", cred.CredID, cred.Hash, computed)
	}

	cred.SharedWithMSP = "Org2MSP"
	b, _ := json.Marshal(cred)
	if err := s.putPDC(ctx, org2PDC, cred.CredID, b); err != nil {
		return err
	}
	return s.putAudit(ctx, cred.CredID, "SHARE_TO_ORG2", "")
}

// RevokeCredential updates Org1’s private record status to "revoked".
func (s *SmartContract) RevokeCredential(ctx contractapi.TransactionContextInterface, credID string) error {
	msp, _ := ctx.GetClientIdentity().GetMSPID()
	if msp != "Org1MSP" {
		return fmt.Errorf("only Org1 can revoke credentials")
	}
	raw, err := s.getPDC(ctx, org1PDC, credID)
	if err != nil {
		return err
	}
	if raw == nil {
		return fmt.Errorf("credential %s not found", credID)
	}

	var cred Credential
	if err := json.Unmarshal(raw, &cred); err != nil {
		return fmt.Errorf("unmarshal credential: %w", err)
	}
	cred.Status = "revoked"
	if cred.SharedWithMSP == "" {
		cred.SharedWithMSP = ""
	}

	b, _ := json.Marshal(cred)
	if err := s.putPDC(ctx, org1PDC, credID, b); err != nil {
		return err
	}
	return s.putAudit(ctx, credID, "REVOKE", "")
}

// CredentialExists checks Org1’s PDC for a key.
func (s *SmartContract) CredentialExists(ctx contractapi.TransactionContextInterface, credID string) (bool, error) {
	raw, err := s.getPDC(ctx, org1PDC, credID)
	if err != nil {
		return false, err
	}
	return raw != nil, nil
}

// ==============================
//             Main
// ==============================

func main() {
	cc, err := contractapi.NewChaincode(new(SmartContract))
	if err != nil {
		panic(fmt.Errorf("create chaincode: %w", err))
	}
	if err := cc.Start(); err != nil {
		panic(fmt.Errorf("start chaincode: %w", err))
	}
}
