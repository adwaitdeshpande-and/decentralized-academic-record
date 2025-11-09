# app.py — polished Streamlit UI for the credential demo

import json
import time
from typing import Tuple, Any, Dict, Optional

import requests
import streamlit as st

# ----------------------------- Config & helpers -----------------------------
if "last_verification" not in st.session_state:
    st.session_state.last_verification = None
    st.session_state.last_verification_id = ""
st.set_page_config(page_title="Blockchain Credential Demo", page_icon="🎓", layout="wide")

# Sidebar: API base + health
st.sidebar.title("⚙️ Settings")
API = st.sidebar.text_input("Gateway API URL", "http://localhost:3000")
TIMEOUT = st.sidebar.number_input("HTTP timeout (sec)", min_value=5, max_value=60, value=15, step=1)

def call_api(
    method: str, path: str, *, json_payload: Optional[Dict[str, Any]] = None, timeout: int = TIMEOUT
) -> Tuple[bool, Any, float]:
    """Return (ok, data/string, elapsed_sec)."""
    url = f"{API.rstrip('/')}/{path.lstrip('/')}"
    t0 = time.time()
    try:
        resp = requests.request(method.upper(), url, json=json_payload, timeout=timeout)
        elapsed = time.time() - t0
        if resp.ok:
            # Try JSON, fall back to text
            try:
                return True, resp.json(), elapsed
            except Exception:
                return True, resp.text, elapsed
        else:
            try:
                err = resp.json()
            except Exception:
                err = resp.text
            return False, err, elapsed
    except Exception as e:
        return False, str(e), time.time() - t0


# Sidebar: health check
col_h1, col_h2 = st.sidebar.columns([1, 1])
if col_h1.button("🔎 Health check"):
    ok, data, sec = call_api("GET", "/health")
    if ok:
        st.sidebar.success(f"Healthy ({sec:.2f}s)")
    else:
        st.sidebar.error(f"Unhealthy: {data}")

st.title("🎓 Blockchain Credential System")

st.caption(
    "Issue → Share → Verify → (optional) Revoke. "
    "Org1 writes to its private collection; Org2 verifies from its own private collection."
)

st.divider()

# ----------------------------- Tabs ----------------------------------------

tab_issue, tab_share, tab_verify, tab_revoke = st.tabs(["📄 Issue", "🔗 Share", "🔍 Verify", "⛔ Revoke"])

# ----------------------------- Issue ---------------------------------------

with tab_issue:
    st.subheader("Issue Credential (University / Org1)")

    with st.form("issue_form"):
        c1, c2, c3 = st.columns([1, 1, 1])
        with c1:
            credID = st.text_input("Credential ID", value="CRED3001")
            studentID = st.text_input("Student ID", value="S-301")
            gpa = st.text_input("GPA", value="8.8")
        with c2:
            studentName = st.text_input("Student Name", value="Asha Patel")
            degree = st.text_input("Degree", value="B.Tech")
            issueDate = st.text_input("Issue Date (YYYY-MM-DD)", value="2025-10-01")
        with c3:
            university = st.text_input("University", value="UniA")
            hashv = st.text_input("Optional Hash", value="hash123")

        submitted = st.form_submit_button("🚀 Issue")
        if submitted:
            if not credID.strip():
                st.error("Credential ID is required.")
            else:
                with st.spinner("Submitting to Org1…"):
                    payload = {
                        "credID": credID.strip(),
                        "studentID": studentID.strip(),
                        "studentName": studentName.strip(),
                        "university": university.strip(),
                        "degree": degree.strip(),
                        "gpa": gpa.strip(),
                        "issueDate": issueDate.strip(),
                        "hash": hashv.strip(),
                    }
                    ok, data, sec = call_api("POST", "/issue", json_payload=payload)
                if ok:
                    st.success(f"✅ Issued {credID}  •  {sec:.2f}s")
                    st.code(json.dumps(data, indent=2), language="json")
                else:
                    st.error("Failed to issue.")
                    st.code(json.dumps(data, indent=2) if isinstance(data, dict) else str(data))

# ----------------------------- Share ---------------------------------------

with tab_share:
    st.subheader("Share Credential (Student identity)")

    with st.form("share_form"):
        c1, c2 = st.columns([1, 1])
        with c1:
            credID2 = st.text_input("Credential ID to Share", value="CRED3001", key="share_cred")
        with c2:
            targetMSP = st.text_input("Target MSP", value="Org2MSP")

        do_share = st.form_submit_button("🔗 Share with Employer")
        if do_share:
            if not credID2.strip():
                st.error("Credential ID is required.")
            else:
                with st.spinner("Sharing (Org1 → Org2)…"):
                    payload = {"credID": credID2.strip(), "targetMSP": targetMSP.strip()}
                    ok, data, sec = call_api("POST", "/share", json_payload=payload)
                if ok:
                    st.success(f"✅ Shared to {targetMSP}  •  {sec:.2f}s")
                    st.code(json.dumps(data, indent=2), language="json")
                else:
                    st.error("Share failed.")
                    st.code(json.dumps(data, indent=2) if isinstance(data, dict) else str(data))

# ----------------------------- Verify --------------------------------------

with tab_verify:
    st.subheader("Verify Credential (Employer / Org2)")

    with st.form("verify_form"):
        credID3 = st.text_input("Credential ID to Verify", value="CRED3001", key="verify_cred")
        do_verify = st.form_submit_button("🔍 Verify")
        if do_verify:
            if not credID3.strip():
                st.error("Credential ID is required.")
            else:
                with st.spinner("Reading from Org2…"):
                    ok, data, sec = call_api("GET", f"/verify/{credID3.strip()}")
                if ok:
                    st.success(f"✅ Verified  •  {sec:.2f}s")
                    ok2, report, _ = call_api("GET", f"/verify-hash/{credID3.strip()}")
                    if ok2 and isinstance(report, dict):
                        badge = "✅" if report.get("isHashValid") else "❌"
                    st.info(f"{badge} Hash check — stored: {report.get('storedHash')[:10]}…  computed: {report.get('computedHash')[:10]}…")
                    st.json(data)
                    # Save for download OUTSIDE the form
                    st.session_state.last_verification = data
                    st.session_state.last_verification_id = credID3.strip()
                else:
                    st.error("Verify failed.")
                    st.code(json.dumps(data, indent=2) if isinstance(data, dict) else str(data))
                    # Clear any previous saved result
                    st.session_state.last_verification = None
                    st.session_state.last_verification_id = ""

    # OUTSIDE the form: show the download button if we have a result
    if st.session_state.last_verification:
        st.download_button(
            "⬇️ Download verification JSON",
            data=json.dumps(st.session_state.last_verification, indent=2),
            file_name=f"{st.session_state.last_verification_id}_verification.json",
            mime="application/json",
            use_container_width=True,
        )


# ----------------------------- Revoke --------------------------------------

with tab_revoke:
    st.subheader("Revoke Credential (University / Org1)")

    with st.form("revoke_form"):
        credID4 = st.text_input("Credential ID to Revoke", value="CRED3001", key="revoke_cred")
        do_revoke = st.form_submit_button("⛔ Revoke")
        if do_revoke:
            if not credID4.strip():
                st.error("Credential ID is required.")
            else:
                with st.spinner("Revoking in Org1…"):
                    ok, data, sec = call_api("POST", "/revoke", json_payload={"credID": credID4.strip()})
                if ok:
                    st.success(f"✅ Revoked  •  {sec:.2f}s")
                    st.code(json.dumps(data, indent=2), language="json")
                else:
                    st.error("Revoke failed.")
                    st.code(json.dumps(data, indent=2) if isinstance(data, dict) else str(data))

# ----------------------------- Footer --------------------------------------

st.divider()
st.caption(
    "Tips: Use fresh Credential IDs for each cycle. If a step fails, check the sidebar health check, "
    "confirm your gateway server is running, and ensure the network is up."
)
