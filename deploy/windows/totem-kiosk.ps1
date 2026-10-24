<#
  Watchdog del kiosk: avvia Chrome in modalità kiosk con un profilo persistente dedicato
  e lo riavvia se si chiude. Gira nella sessione dell'utente del totem (attività
  pianificata "all'accesso", creata da installa-kiosk.ps1).

  Il profilo persistente (--user-data-dir) conserva Cache Storage, IndexedDB e cookie:
  è lì che stanno i documenti offline. Non usare la modalità InPrivate/guest.
#>
param(
  [Parameter(Mandatory = $true)][string]$Url,
  [string]$ProfileDir = 'C:\TotemKiosk\profilo',
  [int]$IntervalSec = 10
)

$ErrorActionPreference = 'Continue'
$log = 'C:\TotemKiosk\watchdog.log'

function Write-Log($msg) {
  $line = "{0:yyyy-MM-dd HH:mm:ss}  {1}" -f (Get-Date), $msg
  Add-Content -Path $log -Value $line
  # rotazione semplice: oltre 1 MB si riparte
  if ((Get-Item $log).Length -gt 1MB) { Move-Item $log "$log.1" -Force }
}

$chrome = @(
  "$env:ProgramFiles\Google\Chrome\Application\chrome.exe",
  "${env:ProgramFiles(x86)}\Google\Chrome\Application\chrome.exe",
  "$env:LOCALAPPDATA\Google\Chrome\Application\chrome.exe"
) | Where-Object { Test-Path $_ } | Select-Object -First 1

if (-not $chrome) { Write-Log 'Chrome non trovato: installarlo per tutti gli utenti'; exit 1 }

New-Item -ItemType Directory -Force -Path $ProfileDir | Out-Null

$arguments = @(
  '--kiosk', $Url,
  "--user-data-dir=`"$ProfileDir`"",
  '--no-first-run',
  '--no-default-browser-check',
  '--noerrdialogs',
  '--disable-session-crashed-bubble',
  '--hide-crash-restore-bubble',
  '--disable-translate',
  '--disable-features=Translate',
  '--overscroll-history-navigation=0'
)

function Test-KioskRunning {
  # Get-Process non mostra la riga di comando in Windows PowerShell 5.1: si usa CIM
  $p = Get-CimInstance Win32_Process -Filter "Name = 'chrome.exe'" -ErrorAction SilentlyContinue |
    Where-Object { $_.CommandLine -and $_.CommandLine -like "*$ProfileDir*" }
  return [bool]$p
}

Write-Log "watchdog avviato: $chrome $Url"
while ($true) {
  if (-not (Test-KioskRunning)) {
    Write-Log 'Chrome non in esecuzione: avvio'
    Start-Process -FilePath $chrome -ArgumentList $arguments
    Start-Sleep -Seconds 5
  }
  Start-Sleep -Seconds $IntervalSec
}
