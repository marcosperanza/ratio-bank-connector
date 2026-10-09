# Set up your own Enable Banking application

Ratio Bank Connector connects to an Enable Banking application that you create and control. Ratio does not provide a shared Enable Banking account or receive your Enable Banking private key. The Connector uses that key locally, then encrypts transaction data before sending it to Ratio.

For personal use, configure your own application and link only accounts you are authorized to access. Enable Banking's production restricted mode returns only accounts linked to that application. Review Enable Banking's current terms and eligibility before using the provider.

## 1. Create an Enable Banking account

Open the [Enable Banking Control Panel](https://enablebanking.com/sign-in/) and sign in with your email. The first sign-in creates the account; access is completed through a one-time link sent by email.

## 2. Register an API application

In **API applications**, choose **Add a new application**. Select **Sandbox** to test with simulated bank data, or **Production** for your own real accounts. Sandbox and Production applications are separate.

Choose the Control Panel option to generate the private key in your browser and export it. Register the application with the Ratio callback URL in its allowed redirect URLs:

```text
https://www.ratio-fin.com/api/connector/eb-callback
```

If you use another Ratio deployment, replace the host with that deployment's origin and callback path. After registration, save the assigned **Application ID** and the downloaded `.pem` file. Store the `.pem` privately on the machine that will run Connector; do not upload it to Ratio, GitHub, cloud storage, or support tickets. If you lose it, rotate the key through Enable Banking's Control Panel.

For the Control Panel's current field names and registration flow, see [Enable Banking's Control Panel guide](https://enablebanking.com/docs/api/control-panel/).

## 3. Activate a Production application in restricted mode

A new Production application starts inactive. In the Control Panel, select **Activate by linking accounts** and complete the bank authorization for each account you intend to use. This activates restricted access for those linked accounts. Accounts not linked to the application are not available through it. Follow [Enable Banking's restricted-account instructions](https://enablebanking.com/docs/api/linked-accounts/).

The user connects to their bank for authorization. Ratio and Connector do not ask for or store the bank login credentials.

## 4. Create a Ratio Connector API key

In Ratio, open **Dashboard → Connector → API keys**, create a key for this installation, and save it securely when it is shown. The key is separate from your Enable Banking App ID and PEM. If it is exposed, revoke it in Ratio and create a replacement.

## 5. Configure Connector locally

Install the release for your operating system using [the installation guide](INSTALLATION.md), then start the add-on setup wizard:

```bash
./bank-connector setup-addon
```

On Windows, run `bank-connector.exe setup-addon`. Enter the Ratio URL, Ratio Connector API key, Enable Banking Application ID, and path to the `.pem` file when prompted. Keep the configuration directory private; it stores credentials needed by the local process. Never paste credentials into issues, logs, screenshots, or chat.

Run the connectivity check:

```bash
./bank-connector doctor --network
```

## 6. Link a bank account through Ratio

Leave the Connector running. In Ratio's **Dashboard → Connector**, add a bank account and choose the institution and country. Connector retrieves the pending setup and opens the bank authorization page. Sign in with the bank there, review the requested access, and approve the account. Enable Banking returns to Ratio through the callback registered above.

Check that the account appears as connected in Ratio and that its first synchronization finishes. New movements arrive in the **Bank Inbox** for review; select the transactions you want to import into your workspace.

## 7. Revoke access

Stop or uninstall Connector, revoke its API key in Ratio, and remove the linked account or application in Enable Banking's Control Panel. Removing the local program alone does not revoke either credential.

## Privacy and provider terms

The Enable Banking private key remains on your machine. Connector encrypts bank transactions locally before pushing them to Ratio. Your AI client or MCP host may receive plaintext only if you separately connect it to local Ratio data; review the host's data handling before enabling that workflow.

Enable Banking controls its account terms, production eligibility, provider availability, and any charges. Check its current [FAQ](https://enablebanking.com/docs/faq/) and [Terms of Service](https://enablebanking.com/terms/) before use. This guide describes the Ratio Connector setup and does not replace those terms.
