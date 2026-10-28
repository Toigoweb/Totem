<#
  Agente del PC del totem. Gira nella sessione dell'utente del totem (attività pianificata
  "all'accesso", creata da installa-kiosk.ps1) accanto al watchdog di Chrome.

  Ogni 30 secondi manda al server lo stato del PC (Chrome, alimentazione, batteria o UPS,
  disco, aggiornamenti in attesa, tempo dall'avvio) e riceve i comandi della manutenzione.
  È il PC a chiamare il server: sul PC non si apre nessuna porta.

  Comandi possibili (elenco chiuso, nient'altro viene eseguito):
    riavvia-chrome, riavvia-pc, schermo-on, schermo-off, screenshot, annuncio
  Inoltre: schermo spento nell'orario notturno (se attivato) e, durante un'emergenza,
  schermo acceso, volume al massimo e annuncio vocale del messaggio.

  Il token sta in C:\TotemKiosk\agente.token, leggibile solo dall'utente del totem e
  dagli amministratori. Non compare mai nel log.

  Prova locale senza server:  totem-agente.ps1 -Server x -Prova
#>
param(
  [Parameter(Mandatory = $true)][string]$Server,
  [string]$TokenFile = 'C:\TotemKiosk\agente.token',
  [string]$ProfileDir = 'C:\TotemKiosk\profilo',
  [string]$LogFile = 'C:\TotemKiosk\agente.log',
  [int]$IntervalSec = 30,
  [switch]$Once,
  [switch]$Prova
)

$ErrorActionPreference = 'Continue'
$Versione = '1.0'
$Server = $Server.TrimEnd('/')

function Write-Log($msg) {
  try {
    $line = "{0:yyyy-MM-dd HH:mm:ss}  {1}" -f (Get-Date), $msg
    Add-Content -Path $LogFile -Value $line -Encoding UTF8
    if ((Get-Item $LogFile).Length -gt 1MB) { Move-Item $LogFile "$LogFile.1" -Force }
  } catch { }
}

# TLS 1.2 (e 1.3 dove c'è): Windows PowerShell 5.1 non lo attiva da solo
try { [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12 -bor 12288 } catch {
  [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
}

Add-Type -AssemblyName System.Windows.Forms, System.Drawing
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class TotemNative {
  [DllImport("user32.dll")] public static extern bool SetProcessDPIAware();
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr hWnd, uint msg, IntPtr wParam, IntPtr lParam);
  [DllImport("user32.dll")] public static extern void keybd_event(byte vk, byte scan, uint flags, UIntPtr extra);
  [DllImport("user32.dll")] public static extern void mouse_event(uint flags, int dx, int dy, uint data, UIntPtr extra);
  public static void MonitorOff() { PostMessage((IntPtr)0xFFFF, 0x0112, (IntPtr)0xF170, (IntPtr)2); }
  public static void MonitorOn() {
    PostMessage((IntPtr)0xFFFF, 0x0112, (IntPtr)0xF170, (IntPtr)(-1));
    mouse_event(0x0001, 1, 0, 0, UIntPtr.Zero);
    mouse_event(0x0001, -1, 0, 0, UIntPtr.Zero);
  }
  public static void VolumeMax() {
    for (int i = 0; i < 50; i++) { keybd_event(0xAF, 0, 0, UIntPtr.Zero); keybd_event(0xAF, 0, 2, UIntPtr.Zero); }
  }
}
'@
[void][TotemNative]::SetProcessDPIAware()

# ---------------------------------------------------------------------------
# Stato del PC

function Test-Chrome {
  $p = Get-CimInstance Win32_Process -Filter "Name = 'chrome.exe'" -ErrorAction SilentlyContinue |
    Where-Object { $_.CommandLine -and $_.CommandLine -like "*$ProfileDir*" }
  return [bool]$p
}

function Get-Stato {
  $st = [ordered]@{ versione = $Versione; chrome = (Test-Chrome); rete = $null; batteria = $null; uptimeMin = 0;
    discoLiberoMB = 0; riavvioInAttesa = $false; windows = ''; schermoSpento = [bool]$script:screenOff; fatti = @($script:done) }
  # alimentazione: anche un UPS collegato in USB compare come batteria
  try {
    $ps = [System.Windows.Forms.SystemInformation]::PowerStatus
    if ($ps.PowerLineStatus -eq 'Online') { $st.rete = $true } elseif ($ps.PowerLineStatus -eq 'Offline') { $st.rete = $false }
    if (-not ($ps.BatteryChargeStatus -band [System.Windows.Forms.BatteryChargeStatus]::NoSystemBattery) -and $ps.BatteryLifePercent -le 1) {
      $st.batteria = [math]::Round($ps.BatteryLifePercent * 100)
    }
  } catch { }
  try {
    $os = Get-CimInstance Win32_OperatingSystem
    $st.uptimeMin = [int]((Get-Date) - $os.LastBootUpTime).TotalMinutes
    $cv = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion' -ErrorAction SilentlyContinue
    $st.windows = ("{0} {1} ({2})" -f $os.Caption, $cv.DisplayVersion, $os.BuildNumber).Replace('Microsoft ', '').Trim()
  } catch { }
  try { $st.discoLiberoMB = [math]::Round((Get-PSDrive -Name ($env:SystemDrive.TrimEnd(':'))).Free / 1MB) } catch { }
  $st.riavvioInAttesa = (Test-Path 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired') -or
    (Test-Path 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending')
  return $st
}

# ---------------------------------------------------------------------------
# Azioni

function Get-ScreenPng {
  $b = [System.Windows.Forms.SystemInformation]::VirtualScreen
  $bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.CopyFromScreen($b.Left, $b.Top, 0, 0, $bmp.Size)
  $g.Dispose()
  # al massimo 1920 px di larghezza: basta per vedere cosa mostra il totem
  if ($bmp.Width -gt 1920) {
    $h = [int]($bmp.Height * 1920 / $bmp.Width)
    $small = New-Object System.Drawing.Bitmap $bmp, 1920, $h
    $bmp.Dispose()
    $bmp = $small
  }
  $ms = New-Object System.IO.MemoryStream
  $bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
  $bmp.Dispose()
  return $ms.ToArray()
}

function Invoke-Annuncio([string]$testo) {
  [TotemNative]::MonitorOn()
  $script:screenOff = $false
  [TotemNative]::VolumeMax()
  for ($i = 0; $i -lt 3; $i++) { [System.Media.SystemSounds]::Exclamation.Play(); Start-Sleep -Milliseconds 700 }
  Add-Type -AssemblyName System.Speech
  $s = New-Object System.Speech.Synthesis.SpeechSynthesizer
  $it = $s.GetInstalledVoices() | Where-Object { $_.Enabled -and $_.VoiceInfo.Culture.Name -like 'it*' } | Select-Object -First 1
  if ($it) { $s.SelectVoice($it.VoiceInfo.Name) } else { Write-Log 'nessuna voce italiana installata: annuncio con la voce predefinita' }
  $s.Volume = 100
  $s.Rate = -1
  $s.Speak($testo)
  $s.Dispose()
}

function Send-Request([string]$path, [byte[]]$body, [string]$ctype) {
  $r = Invoke-WebRequest -UseBasicParsing -Method Post -Uri "$Server$path" -TimeoutSec 20 `
    -Headers @{ Authorization = "Bearer $script:token" } -ContentType $ctype -Body $body
  # decodifica esplicita in UTF-8: Windows PowerShell 5.1 a volte sbaglia le lettere accentate
  return [Text.Encoding]::UTF8.GetString($r.RawContentStream.ToArray()) | ConvertFrom-Json
}

function Invoke-Comando($c) {
  try {
    switch ($c.azione) {
      'riavvia-chrome' {
        Get-CimInstance Win32_Process -Filter "Name = 'chrome.exe'" | Where-Object { $_.CommandLine -like "*$ProfileDir*" } |
          ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
        # il watchdog lo riapre entro pochi secondi
      }
      'riavvia-pc' {
        $script:done += @{ id = $c.id; ok = $true }
        Send-Report | Out-Null
        Write-Log 'riavvio del PC richiesto dalla manutenzione'
        shutdown.exe /r /t 30 /c "Riavvio richiesto dalla manutenzione del totem"
        return
      }
      'schermo-off' { [TotemNative]::MonitorOff(); $script:screenOff = $true }
      'schermo-on' { [TotemNative]::MonitorOn(); $script:screenOff = $false }
      'screenshot' { Send-Request '/api/agent/screenshot' (Get-ScreenPng) 'image/png' | Out-Null }
      'annuncio' { Invoke-Annuncio ([string]$c.testo) }
      default { throw "azione sconosciuta: $($c.azione)" }
    }
    $script:done += @{ id = $c.id; ok = $true }
    Write-Log "comando eseguito: $($c.azione)"
  } catch {
    $script:done += @{ id = $c.id; ok = $false; errore = "$_" }
    Write-Log "comando non riuscito: $($c.azione): $_"
  }
}

function Send-Report {
  $json = Get-Stato | ConvertTo-Json -Depth 4 -Compress
  $script:done = @()
  return Send-Request '/api/agent/report' ([Text.Encoding]::UTF8.GetBytes($json)) 'application/json; charset=utf-8'
}

# ---------------------------------------------------------------------------

$script:done = @()
$script:screenOff = $false
$script:nightOff = $false
$script:nightOffAt = Get-Date
$script:lastEmergency = ''

if ($Prova) {
  # prova locale: stato del PC e screenshot in un file, nessuna chiamata al server
  Get-Stato | ConvertTo-Json -Depth 4
  $png = Get-ScreenPng
  [IO.File]::WriteAllBytes((Join-Path $env:TEMP 'totem-agente-prova.png'), $png)
  Write-Host "screenshot: $((Join-Path $env:TEMP 'totem-agente-prova.png')) ($($png.Length) byte)"
  exit 0
}

if (-not (Test-Path $TokenFile)) { Write-Log "token mancante: $TokenFile"; exit 1 }
$script:token = (Get-Content -Raw $TokenFile).Trim()
Write-Log "agente $Versione avviato verso $Server"

$failures = 0
while ($true) {
  try {
    $resp = Send-Report
    if ($failures -ge 3) { Write-Log 'server di nuovo raggiungibile' }
    $failures = 0
    foreach ($c in @($resp.comandi)) { if ($c) { Invoke-Comando $c } }

    # emergenza: una sola volta per emergenza (la ripetizione arriva come comando)
    if ($resp.emergenza -and $resp.emergenza.id -ne $script:lastEmergency) {
      $script:lastEmergency = $resp.emergenza.id
      Write-Log "emergenza: $($resp.emergenza.titolo)"
      try { Invoke-Annuncio ("{0}. {1}" -f $resp.emergenza.titolo, $resp.emergenza.testo) } catch { Write-Log "annuncio non riuscito: $_" }
    } elseif (-not $resp.emergenza) {
      $script:lastEmergency = ''
    }

    # schermo di notte: si spegne all'inizio della fascia e di nuovo 10 minuti dopo un
    # eventuale tocco; si riaccende alla fine
    if ($resp.schermoSpento) {
      if (-not $script:nightOff -or ((Get-Date) - $script:nightOffAt).TotalMinutes -ge 10) {
        [TotemNative]::MonitorOff()
        $script:nightOff = $true; $script:screenOff = $true; $script:nightOffAt = Get-Date
      }
    } elseif ($script:nightOff) {
      [TotemNative]::MonitorOn()
      $script:nightOff = $false; $script:screenOff = $false
    }
  } catch {
    $failures++
    if ($failures -eq 3) { Write-Log "server non raggiungibile: $($_.Exception.Message)" }
    if ("$_" -match '401') { Write-Log 'token rifiutato dal server: rigenerarlo in Manutenzione → Dispositivi → PC Windows' }
  }
  if ($Once) { break }
  Start-Sleep -Seconds $IntervalSec
}
