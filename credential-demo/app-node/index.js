// index.js (drop-in)
// Node >= 18, package.json has:  "type": "module"
// npm deps: express, @grpc/grpc-js, @hyperledger/fabric-gateway

import express from "express";
import fs from "node:fs";
import path from "node:path";
import * as grpc from "@grpc/grpc-js";
import { connect, signers } from "@hyperledger/fabric-gateway";
import { createPrivateKey } from "node:crypto";

const app = express();
app.use(express.json());

// ----- Paths to test-network artifacts -----
const TESTNET = path.resolve("../../fabric-samples/test-network");

// Robustly load CCPs
function loadJSON(p) {
  return JSON.parse(fs.readFileSync(p, "utf8"));
}
const CCP_ORG1 = loadJSON(
  path.join(TESTNET, "organizations/peerOrganizations/org1.example.com/connection-org1.json")
);
const CCP_ORG2 = loadJSON(
  path.join(TESTNET, "organizations/peerOrganizations/org2.example.com/connection-org2.json")
);

// ----- Helpers to open Gateway/Contract -----
function getPeerInfo(ccp) {
  const [peerName] = Object.keys(ccp.peers);
  const peer = ccp.peers[peerName];
  const endpoint = peer.url.replace("grpcs://", "");
  const tlsRootCert = Buffer.from(peer.tlsCACerts.pem);
  const hostOverride = peerName;
  return { endpoint, tlsRootCert, hostOverride };
}

function userPath(org, userDir) {
  return path.join(
    TESTNET,
    `organizations/peerOrganizations/${org}.example.com/users/${userDir}`
  );
}

function loadIdentity(mspId, uPath) {
  const certDir = path.join(uPath, "msp", "signcerts");
  const keyDir = path.join(uPath, "msp", "keystore");
  const [certFile] = fs.readdirSync(certDir);
  const [keyFile] = fs.readdirSync(keyDir);
  const certificate = fs.readFileSync(path.join(certDir, certFile));
  const privateKey = createPrivateKey(fs.readFileSync(path.join(keyDir, keyFile), "utf8"));
  return { mspId, certificate, signer: signers.newPrivateKeySigner(privateKey) };
}

async function getContract({ msp, userDir, channel = "mychannel", chaincode = "cred" }) {
  const ccp = msp === "Org1MSP" ? CCP_ORG1 : CCP_ORG2;
  const { endpoint, tlsRootCert, hostOverride } = getPeerInfo(ccp);
  const creds = grpc.credentials.createSsl(tlsRootCert);
  const client = new grpc.Client(endpoint, creds, {
    "grpc.ssl_target_name_override": hostOverride,
    "grpc.default_authority": hostOverride,
  });

  const uPath =
    msp === "Org1MSP" ? userPath("org1", userDir) : userPath("org2", userDir);
  const { mspId, certificate, signer } = loadIdentity(msp, uPath);

  const gateway = await connect({
    client,
    identity: { mspId, credentials: certificate },
    signer,
  });

  const network = gateway.getNetwork(channel);
  const contract = network.getContract(chaincode);
  return { gateway, contract };
}

// ----- Utilities -----
function errPayload(e) {
  return e?.details || e?.message || String(e);
}

app.get("/health", (_req, res) => res.json({ ok: true }));

// ----- ISSUE (Org1 writes to Org1 PDC) -----
app.post("/issue", async (req, res) => {
  let gateway;
  try {
    const {
      credID,
      studentID,
      studentName,
      university,
      degree,
      gpa,
      issueDate,
      hash = "",
    } = req.body;

    ({ gateway, contract: (() => { }) }); // placeholder to satisfy linter
    const r = await getContract({
      msp: "Org1MSP",
      userDir: "Admin@org1.example.com",
    });
    gateway = r.gateway;
    const contract = r.contract;

    const proposal = contract.newProposal("IssueCredential", {
      arguments: [credID, studentID, studentName, university, degree, gpa, issueDate, hash],
    });

    const endorsed = await proposal.endorse({ endorsingOrganizations: ["Org1MSP"] });
    const commit = await endorsed.submit();
    await commit.getStatus(); // throws if not VALID

    res.json({ ok: true, credID });
  } catch (e) {
    console.error("Issue error:", e);
    res.status(500).json({ error: errPayload(e) });
  } finally {
    try { gateway?.close(); } catch { /* ignore */ }
  }
});

// ----- SHARE (two-step: read as Org1, then write as Org2) -----
app.post("/share", async (req, res) => {
  let g1, g2;
  try {
    const { credID } = req.body;
    if (!credID) return res.status(400).json({ error: "credID is required" });

    // 1) Read from Org1 private collection (Org1 identity)
    {
      const r1 = await getContract({ msp: "Org1MSP", userDir: "Admin@org1.example.com" });
      g1 = r1.gateway;
      const c1 = r1.contract;

      const pRead = c1.newProposal("ReadCredential", { arguments: [credID] });
      const buf = await pRead.evaluate({ endorsingOrganizations: ["Org1MSP"] });
      var credObj = JSON.parse(Buffer.from(buf).toString("utf8"));
      g1.close();
      g1 = undefined;
    }

    // 2) Write into Org2 private collection (Org2 identity)
    {
      const r2 = await getContract({ msp: "Org2MSP", userDir: "Admin@org2.example.com" });
      g2 = r2.gateway;
      const c2 = r2.contract;

      const credJSON = JSON.stringify(credObj);
      const pWrite = c2.newProposal("StoreCredentialForOrg2", { arguments: [credJSON] });
      const endorsed = await pWrite.endorse({ endorsingOrganizations: ["Org2MSP"] });
      const commit = await endorsed.submit();
      await commit.getStatus();
      g2.close();
      g2 = undefined;
    }

    res.json({ ok: true, credID, sharedTo: "Org2MSP" });
  } catch (e) {
    try { g1?.close(); } catch {}
    try { g2?.close(); } catch {}
    console.error("Share error:", e);
    res.status(500).json({ error: e?.details || e?.message || String(e) });
  }
});

// ----- VERIFY (Org2 reads from Org2 PDC) -----
app.get("/verify/:credID", async (req, res) => {
  let gateway;
  try {
    const r = await getContract({
      msp: "Org2MSP",
      userDir: "Admin@org2.example.com",
    });
    gateway = r.gateway;
    const contract = r.contract;

    const proposal = contract.newProposal("VerifyCredential", {
      arguments: [req.params.credID],
    });
    const resultBuf = await proposal.evaluate({ endorsingOrganizations: ["Org2MSP"] });
    const result = JSON.parse(Buffer.from(resultBuf).toString("utf8"));
    res.json(result);
  } catch (e) {
    console.error("Verify error:", e);
    res.status(403).json({ error: errPayload(e) });
  } finally {
    try { gateway?.close(); } catch { /* ignore */ }
  }
});

// ----- REVOKE (two-step: Org1 revoke -> Org1 read -> Org2 upsert) -----
app.post("/revoke", async (req, res) => {
  let g1, g2;
  try {
    const { credID } = req.body;
    if (!credID) return res.status(400).json({ error: "credID is required" });

    // 1) Revoke in Org1 (updates Org1 PDC)
    {
      const r1 = await getContract({ msp: "Org1MSP", userDir: "Admin@org1.example.com" });
      g1 = r1.gateway;
      const c1 = r1.contract;

      const pRevoke = c1.newProposal("RevokeCredential", { arguments: [credID] });
      const endorsed = await pRevoke.endorse({ endorsingOrganizations: ["Org1MSP"] });
      const commit = await endorsed.submit();
      await commit.getStatus();

      // Read back the updated record from Org1
      const pRead = c1.newProposal("ReadCredential", { arguments: [credID] });
      const buf = await pRead.evaluate({ endorsingOrganizations: ["Org1MSP"] });
      var updated = JSON.parse(Buffer.from(buf).toString("utf8"));

      g1.close(); g1 = undefined;
    }

    // 2) Upsert into Org2 (so Verify reflects the revoked status)
    {
      const r2 = await getContract({ msp: "Org2MSP", userDir: "Admin@org2.example.com" });
      g2 = r2.gateway;
      const c2 = r2.contract;

      const pWrite = c2.newProposal("StoreCredentialForOrg2", { arguments: [JSON.stringify(updated)] });
      const endorsed2 = await pWrite.endorse({ endorsingOrganizations: ["Org2MSP"] });
      const commit2 = await endorsed2.submit();
      await commit2.getStatus();

      g2.close(); g2 = undefined;
    }

    res.json({ ok: true, credID, status: "revoked", syncedTo: "Org2MSP" });
  } catch (e) {
    try { g1?.close(); } catch {}
    try { g2?.close(); } catch {}
    console.error("Revoke error:", e);
    res.status(500).json({ error: e?.details || e?.message || String(e) });
  }
});

// GET /verify-hash/:credID  (Org2)
app.get("/verify-hash/:credID", async (req, res) => {
  let gateway;
  try {
    const r = await getContract({ msp: "Org2MSP", userDir: "Admin@org2.example.com" });
    gateway = r.gateway; const c = r.contract;
    const p = c.newProposal("VerifyCredentialIntegrity", { arguments: [req.params.credID] });
    const buf = await p.evaluate({ endorsingOrganizations: ["Org2MSP"] });
    res.json(JSON.parse(Buffer.from(buf).toString("utf8")));
  } catch (e) {
    res.status(500).json({ error: e?.details || e?.message || String(e) });
  } finally { try { gateway?.close(); } catch {} }
});

// ----- Start server -----
const PORT = process.env.PORT || 3000;
app.listen(PORT, () => {
  console.log(`✅ API listening on http://localhost:${PORT}`);
});
