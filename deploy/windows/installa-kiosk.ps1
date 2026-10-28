<#
  Prepara il PC Windows come totem (fase A). Da eseguire una volta, come amministratore:

    powershell -ExecutionPolicy Bypass -File installa-kiosk.ps1 -Url https://totem.azienda.it -Utente totem
    (facoltativo, per il monitoraggio del PC)  -TokenAgente <token>
    (facoltativo, per l'interfono)             -Interfono

  Fa:
    - copia il watchdog in C:\TotemKiosk
    - attività pianificata all'accesso dell'utente del totem: avvio del watchdog
    - attività pianificata ogni notte alle 03:30: chiusura di Chrome (il watchdog lo riapre)
    - alimentazione: mai sospensione né ibernazione, schermo sempre acceso
    - con -TokenAgente: agente del PC (stato, comandi, schermo di notte, annuncio vocale
      di emergenza), token in C:\TotemKiosk\agente.token leggibile solo dall'utente del
      totem e dagli amministratori. Il token si genera in Manutenzione → Dispositivi → PC
    - con -Interfono: criteri di Chrome che consentono al solo indirizzo del totem il
      microfono senza richiesta (mai la webcam) e la suoneria senza un tocco prima
  Non fa (vedi docs/04-messa-in-servizio.md): creazione dell'utente, Autologon,
  criteri di gruppo, Windows Update.
#>
param(
  [Parameter(Mandatory = $true)][string]$Url,
  [string]$Utente = 'totem',
  [string]$OraRiavvio = '03:30',
  [string]$TokenAgente = '',
  [switch]$Interfono
)

$ErrorActionPreference = 'Stop'
if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  throw 'Eseguire come amministratore.'
}

$dir = 'C:\TotemKiosk'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
Copy-Item -Path (Join-Path $PSScriptRoot 'totem-kiosk.ps1') -Destination $dir -Force
# l'utente del totem deve poter scrivere il profilo e il log
icacls $dir /grant "${env:COMPUTERNAME}\${Utente}:(OI)(CI)M" | Out-Null

$principal = New-ScheduledTaskPrincipal -UserId "$env:COMPUTERNAME\$Utente" -LogonType Interactive -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
  -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -MultipleInstances IgnoreNew

# 1) watchdog all'accesso
$ps = "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$dir\totem-kiosk.ps1`" -Url `"$Url`""
$act = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument $ps
$trg = New-ScheduledTaskTrigger -AtLogOn -User "$env:COMPUTERNAME\$Utente"
Register-ScheduledTask -TaskName 'Totem - kiosk' -Action $act -Trigger $trg -Principal $principal -Settings $settings -Force | Out-Null

# 2) riavvio notturno del browser
$stop = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument '-NoProfile -WindowStyle Hidden -Command "Stop-Process -Name chrome -Force -ErrorAction SilentlyContinue"'
$night = New-ScheduledTaskTrigger -Daily -At $OraRiavvio
Register-ScheduledTask -TaskName 'Totem - riavvio notturno browser' -Action $stop -Trigger $night -Principal $principal -Settings $settings -Force | Out-Null

# 3) agente del PC (facoltativo)
if ($TokenAgente) {
  Copy-Item -Path (Join-Path $PSScriptRoot 'totem-agente.ps1') -Destination $dir -Force
  $tf = Join-Path $dir 'agente.token'
  Set-Content -Path $tf -Value $TokenAgente.Trim() -Encoding ASCII -NoNewline
  # solo l'utente del totem (che esegue l'agente), SYSTEM e amministratori
  icacls $tf /inheritance:r /grant:r "${env:COMPUTERNAME}\${Utente}:R" '*S-1-5-18:F' '*S-1-5-32-544:F' | Out-Null
  $base = $Url -replace '^(https?://[^/]+).*$', '$1'
  $pa = "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$dir\totem-agente.ps1`" -Server `"$base`""
  $aact = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument $pa
  Register-ScheduledTask -TaskName 'Totem - agente' -Action $aact -Trigger $trg -Principal $principal -Settings $settings -Force | Out-Null
  Write-Host "Agente del PC installato: compare in Manutenzione → Dispositivi entro un minuto dall'accesso."
}

# 4) interfono (facoltativo): microfono e suoneria per il solo indirizzo del totem
if ($Interfono) {
  $origin = $Url -replace '^(https?://[^/]+).*$', '$1'
  foreach ($k in 'AudioCaptureAllowedUrls', 'AutoplayAllowlist') {
    $key = "HKLM:\SOFTWARE\Policies\Google\Chrome\$k"
    New-Item -Path $key -Force | Out-Null
    New-ItemProperty -Path $key -Name '1' -Value $origin -PropertyType String -Force | Out-Null
  }
  Write-Host "Interfono: microfono e suoneria consentiti a $origin (verificare in chrome://policy)."
}

# 5) alimentazione (rete elettrica)
powercfg /change standby-timeout-ac 0
powercfg /change hibernate-timeout-ac 0
powercfg /change monitor-timeout-ac 0
powercfg /hibernate off

Write-Host "Fatto. Attività pianificate create per $env:COMPUTERNAME\$Utente; riavvio del browser ogni notte alle $OraRiavvio."
Write-Host 'Proseguire con la checklist: Autologon, criteri di gruppo, Windows Update.'
