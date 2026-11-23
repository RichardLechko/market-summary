<#
.SYNOPSIS
Builds market-summary.exe and registers a Windows scheduled task that runs it
at every logon and daily at 09:00. The program itself decides whether a digest
is due (7+ calendar days since the last successful send) and exits silently
otherwise, so frequent triggers are safe.

.EXAMPLE
pwsh -File scripts\install-task.ps1

.EXAMPLE
Unregister-ScheduledTask -TaskName MarketSummary -Confirm:$false
#>
param(
    [string]$Root = (Split-Path -Parent $PSScriptRoot),
    [string]$TaskName = 'MarketSummary'
)

$ErrorActionPreference = 'Stop'
Push-Location $Root
try {
    go build -o market-summary.exe ./cmd/market-summary
    if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
} finally {
    Pop-Location
}

$exe = Join-Path $Root 'market-summary.exe'
$action = New-ScheduledTaskAction -Execute $exe -Argument 'run' -WorkingDirectory $Root

$logon = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$logon.Delay = 'PT2M'   # give the network a moment after login
$daily = New-ScheduledTaskTrigger -Daily -At 9am

$settings = New-ScheduledTaskSettingsSet `
    -StartWhenAvailable `
    -RunOnlyIfNetworkAvailable `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -ExecutionTimeLimit (New-TimeSpan -Minutes 45) `
    -MultipleInstances IgnoreNew

$principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Limited

Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger @($logon, $daily) `
    -Settings $settings -Principal $principal -Force `
    -Description 'Builds and emails the market summary when 7+ days have passed since the last one.' | Out-Null

Write-Host "Registered scheduled task '$TaskName' running $exe"
