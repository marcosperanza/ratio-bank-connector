$ErrorActionPreference = "Stop"
$TaskName = "Ratio Bank Connector"
$InstallDir = Join-Path $env:LOCALAPPDATA "Ratio\BankConnector"

Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force $InstallDir -ErrorAction SilentlyContinue
Write-Host "Ratio Bank Connector removed. Configuration and local data were preserved."
