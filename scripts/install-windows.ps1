param(
  [string]$SourceBinary = ".\bank-connector.exe"
)

$ErrorActionPreference = "Stop"
$InstallDir = Join-Path $env:LOCALAPPDATA "Ratio\BankConnector"
$BinaryPath = Join-Path $InstallDir "bank-connector.exe"
$ConfigPath = Join-Path $env:APPDATA "Ratio\bank-connector\config.yaml"
$TaskName = "Ratio Bank Connector"

if (-not (Test-Path $SourceBinary)) {
  throw "Binary not found: $SourceBinary"
}
if (-not (Test-Path $ConfigPath)) {
  throw "Configuration not found: $ConfigPath. Run '$SourceBinary setup-addon' first."
}

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Copy-Item -Force $SourceBinary $BinaryPath

$Action = New-ScheduledTaskAction -Execute $BinaryPath -Argument "sync --daemon --config `"$ConfigPath`""
$Trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$Settings = New-ScheduledTaskSettingsSet -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit (New-TimeSpan -Days 3650)
Register-ScheduledTask -TaskName $TaskName -Action $Action -Trigger $Trigger -Settings $Settings -Description "Sync bank transactions securely with Ratio" -Force | Out-Null
Start-ScheduledTask -TaskName $TaskName
Write-Host "Ratio Bank Connector installed and started."
