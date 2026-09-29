# USBridge Remote

<div align="center">
  <img src="./assets/banner6.svg" width="1400" alt="USBridge Remote">
</div>

<div align="center">

[English](README.md) | [Deutsch](docs/README_DE.md) | [Français](docs/README_FR.md) | [Italiano](docs/README_IT.md) | [Español](docs/README_ES.md) | [Português (Brasil)](docs/README_PT_BR.md) | [Українська](docs/README_UA.md) | [Polski](docs/README_PL.md) | [日本語](docs/README_JA.md) | [한국어](docs/README_KO.md) | [简体中文](docs/README_ZH.md)

[![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)](#)
[![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)](#)
[![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)](#)
[![Android](https://img.shields.io/badge/Android-3DDC84?logo=android&logoColor=white)](https://play.google.com/store/apps/details?id=io.usbridge.client)
[![iOS](https://img.shields.io/badge/iOS-000000?logo=apple&logoColor=white)](https://apps.apple.com/us/app/usbridge-remote-desktop/id6787665935)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.com/invite/xqQ6ybkfWS)

</div>

---

**USBridge Remote** è un client unificato ad alte prestazioni per la gestione di macchine remote. Progettato per combinare l'**accesso BIOS a livello hardware** (tramite dispositivi USBridge KVM) e il **desktop remoto basato su software** in un'unica interfaccia semplificata.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote">
</div>


## Download

### Client
Il Client è l'interfaccia di controllo — installata sulla tua workstation o laptop (o eseguita direttamente nel tuo browser). Gestisce le connessioni, il desktop remoto live, il passthrough di dispositivi virtuali e il registro degli snapshot.

| Architettura | Windows | macOS | Linux | Android | iOS | Web Browser |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Open App](https://web.usbridge.io) |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Open App](https://web.usbridge.io) |

Preferisci un APK diretto senza un account Play Store? Una build auto-aggiornante è anche pubblicata nell'[ultima release](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

🌐 **Client Web Zero-Install**: Nessuna installazione richiesta. Basta aprire [web.usbridge.io](https://web.usbridge.io) per connettersi istantaneamente. *(Nota: Il client web opera con alcune limitazioni di funzionalità e prestazioni a causa della sandbox di sicurezza del browser e delle restrizioni di WebRTC. Per un'esperienza completa e senza compromessi, utilizza le app native).* Su un Agente appena avviato, può richiedere fino a un minuto per diventare raggiungibile la prima volta (o dopo un cambiamento di rete) mentre provvede a un certificato HTTPS affidabile per se stesso — vedere la riga **Stato → Certificato** dell'Agente, o la [documentazione dell'Agente](agent/docs/README.md#platform-notes-from-the-top-level-readme) per dettagli.

## Agent

L'Agente gira sulla macchina target — il server o PC a cui vuoi accedere in remoto. Gestisce la cattura dello schermo, l'iniezione di input e la rete Tailscale.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>Architettura</th>
          <th>Windows</th>
          <th>macOS</th>
          <th>Linux</th>
        </tr>
        <tr>
          <td><b>x86_64</b></td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">Download</a></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">Download</a></td>
        </tr>
        <tr>
          <td><b>ARM64</b></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">Download</a></td>
          <td>—</td>
        </tr>
      </table>
    </td>
    <!-- Right Column: Image -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="USBridge Agent Interface" width="100%">
    </td>
  </tr>
</table>

## Features

<table>
  <tr>
    <td width="33%" valign="top">
      <code>FUNZIONALITÀ BASE</code><br><br>
      <b>SUNSHINE OPEN-SOURCE</b><br>
      <i>Streaming di Giochi Standard</i><br><br>
      ✓ Streamer Sunshine open-source integrato<br>
      ✓ Accesso desktop e gaming a bassa latenza<br>
      ✓ Appunti file e testo bidirezionali<br>
      ✓ Supporto per gamepad<br>      
      ✓ Commutazione display multi-monitor<br>
      ✓ Rete P2P Tailscale integrata<br>
      ✓ Supporto Wayland nativo (cattura senza prompt)
    </td>
    <td width="33%" valign="top">
      <code>+ FUNZIONALITÀ BASE</code><br><br>
      <b>USBRIDGE FREE</b><br>
      <i>Motore Remoto Personalizzato</i><br><br>
      ✓ Protocollo remoto personalizzato per connessione istantanea<br>
      ✓ Streaming adattivo ottimizzato per stabilità Wi-Fi<br>
      ✓ Gestione display virtuale headless<br>
      ✓ Passthrough controller gamepad a bassa latenza<br>
      ✓ Accesso client web browser (nessuna installazione richiesta)<br>
      ✓ Accesso pre-login a Windows (inserimento credenziali sicure)
    </td>
    <td width="33%" valign="top">
      <code>+ FUNZIONALITÀ BASE</code> <code>+ FUNZIONALITÀ FREE</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Flussi di Lavoro Professionali</i><br><br>
      ✓ Accuratezza del colore chroma 4:4:4 senza perdita<br>
      ✓ Passthrough di dispositivi periferici USB raw<br>
      ✓ Supporto per tablet grafici con pressione e inclinazione<br><br>
    </td>
  </tr>
</table>

## Quick Start

1. **Installa l'Agente** sulla macchina a cui vuoi accedere in remoto. Avvialo — mostrerà un token di connessione e un indirizzo Tailscale. Connetti Tailscale se hai bisogno di accesso su Internet.

2. **Installa il Client** sulla tua workstation, laptop o telefono.

3. **Aggiungi una connessione** — inserisci l'indirizzo IP o Tailscale mostrato nella finestra dell'Agente. È tutto.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote">
</div>

## Integrazione Hardware

<table>
  <tr>
   <td width="50%" valign="top">
      <b>Controllo BIOS a Livello Hardware</b><br>
      USBridge Remote si integra nativamente con l'apparecchiatura USBridge-KVM 2.0 per la gestione out-of-band, bare-metal prima dell'avvio del sistema operativo.<br><br>
      <a href="https://www.usbridge.io/hardware-agent#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Buy USBridge-KVM 2.0"></a>
    </td>
    <td width="50%" valign="top">
      <b>Firmware IP-KVM Fai-da-Te</b><br>
      Distribuisci il firmware ufficiale su un SBC compatibile (ad esempio, Radxa Zero 3W/3E, Cubie A7) con un'interfaccia di cattura USB per provvedere a un nodo KVM personalizzato.<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Get the Firmware"></a>
    </td>
  </tr>
</table>

## Risorse & Licenza

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Sviluppo & Roadmap</b><br>
      Mantengo un dashboard pubblico per tracciare tutte le funzionalità pianificate, gli aggiornamenti architettonici e i programmi di rilascio.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">Visualizza la Roadmap Live</a>
    </td>
    <td width="33%" valign="top">
      <b>Link al Progetto</b><br>
      <ul>
        <li><a href="https://usbridge.io">Sito Ufficiale</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (Beta Testing & Bugs)</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Supporto Patreon</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>Licenza (GPLv3)</b><br>
      Questo progetto è concesso in licenza sotto la <b>GPLv3</b>. Il client multi-piattaforma incorpora il codice sorgente di <code>moonlight-common-c</code> (anch'esso GPLv3).<br><br>
      <a href="LICENSE">Visualizza il file LICENSE</a>
    </td>
  </tr>
</table>