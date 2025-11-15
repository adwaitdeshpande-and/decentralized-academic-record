# 🧾 Blockchain Credential Verification System  
_A decentralized academic record & verification demo using Hyperledger Fabric, Node.js Gateway, and Streamlit UI._

---

## 🚀 Project Overview
This project demonstrates a complete end-to-end academic credential system using:
- **Hyperledger Fabric** for blockchain network
- **Private Data Collections** for Org1 (University) & Org2 (Employer)
- **Node.js API Gateway** for transaction access
- **Streamlit UI** for user interaction (issue, share, verify, revoke)
- **Audit trail + Auto-hashing** for integrity verification

---

## 🧩 Tech Stack
| Layer | Technology |
|-------|-------------|
| Blockchain | Hyperledger Fabric v2.5 |
| Chaincode | Go (smart contract) |
| Gateway API | Node.js (Express + Fabric Gateway SDK) |
| Frontend | Python Streamlit |
| DB | Ledger state (no external DB) |

---

## ⚙️ Setup Instructions

### 1. Prerequisites
Install Docker, Docker Compose, Node.js (≥18), Go (≥1.20), Python (≥3.10), and Fabric binaries.

```bash
curl -sSL https://raw.githubusercontent.com/hyperledger/fabric/main/scripts/install-fabric.sh -o install-fabric.sh
chmod +x install-fabric.sh

# This pulls: 1) Fabric client binaries, 2) Docker images, 3) the samples repo with test-network
./install-fabric.sh binary docker samples
2. Start Fabric Test Network
bash
Copy code
cd fabric-samples/test-network
./network.sh down
./network.sh up -ca
./network.sh createChannel
3. Deploy Chaincode
bash
Copy code
./network.sh deployCC \
  -ccn cred \
  -ccp ../../credential-demo/chaincode-go \
  -ccl go \
  -cccg ../../credential-demo/chaincode-go/collections_config.json \
  -ccs 1 -ccv 1.0 \
  -ccep "OR('Org1MSP.peer','Org2MSP.peer')"
4. Run the Node API
bash
Copy code
cd credential-demo/app-node
npm install
npm start
5. Run the Streamlit UI
bash
Copy code
cd credential-demo/ui-streamlit
python3 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
streamlit run app.py
Visit: http://localhost:8501

📘 Functional Flow
Action	Performed by	Description
Issue	Org1	Create credential with auto SHA-256 hash
Share	Org1 → Org2	Copy private data securely
Verify	Org2	View + validate hash
Revoke	Org1	Mark revoked + sync Org2
History	Public	Audit trail of ISSUE/SHARE/REVOKE