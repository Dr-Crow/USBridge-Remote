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

**USBridge Remote** est un client unifié haute performance pour gérer des machines distantes. Conçu pour combiner l'**accès BIOS au niveau matériel** (via les appareils USBridge KVM) et le **bureau à distance basé sur logiciel** dans une interface unique et rationalisée.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote">
</div>


## Télécharger

### Client
Le Client est l'interface de contrôle — installé sur votre station de travail ou votre ordinateur portable (ou exécuté directement dans votre navigateur). Il gère les connexions, le bureau à distance en direct, le passage à travers des dispositifs virtuels et le registre des instantanés.

| Architecture | Windows | macOS | Linux | Android | iOS | Navigateur Web |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Télécharger](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Télécharger](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Ouvrir l'App](https://web.usbridge.io) |
| **ARM64** | — | [Télécharger](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Ouvrir l'App](https://web.usbridge.io) |

Vous préférez un APK direct sans compte Play Store ? Une version auto-mise à jour est également publiée dans la [dernière version](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

🌐 **Client Web sans installation** : Aucune installation requise. Ouvrez simplement [web.usbridge.io](https://web.usbridge.io) pour vous connecter instantanément. *(Remarque : Le client web fonctionne avec certaines limitations de fonctionnalités et de performances en raison de la sécurité du navigateur et des contraintes WebRTC. Pour une expérience complète et sans compromis, utilisez les applications natives).* Sur un Agent fraîchement démarré, il peut falloir jusqu'à une minute pour devenir accessible la première fois (ou après un changement de réseau) pendant qu'il provisionne un certificat HTTPS de confiance pour lui-même — consultez la ligne **Statut → Certificat** de l'Agent, ou la [documentation de l'Agent](agent/docs/README.md#platform-notes-from-the-top-level-readme) pour plus de détails.

## Agent

L'Agent s'exécute sur la machine cible — le serveur ou le PC que vous souhaitez accéder à distance. Il gère la capture d'écran, l'injection d'entrée et le réseau Tailscale.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>Architecture</th>
          <th>Windows</th>
          <th>macOS</th>
          <th>Linux</th>
        </tr>
        <tr>
          <td><b>x86_64</b></td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">Télécharger</a></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">Télécharger</a></td>
        </tr>
        <tr>
          <td><b>ARM64</b></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">Télécharger</a></td>
          <td>—</td>
        </tr>
      </table>
    </td>
    <!-- Right Column: Image -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="Interface de l'Agent USBridge" width="100%">
    </td>
  </tr>
</table>

## Fonctionnalités

<table>
  <tr>
    <td width="33%" valign="top">
      <code>FONCTIONNALITÉ DE BASE</code><br><br>
      <b>SUNSHINE OPEN-SOURCE</b><br>
      <i>Streaming de jeux standard</i><br><br>
      ✓ Streamer Sunshine open-source intégré<br>
      ✓ Accès bureau & jeux à faible latence<br>
      ✓ Presse-papiers de fichiers & texte bidirectionnels<br>    
      ✓ Commutation d'affichage multi-moniteurs<br>
      ✓ Réseau P2P Tailscale intégré<br>
      ✓ Support natif Wayland (capture sans invite)
    </td>
    <td width="33%" valign="top">
      <code>+ FONCTIONNALITÉS DE BASE</code><br><br>
      <b>USBRIDGE GRATUIT</b><br>
      <i>Moteur de streaming basé sur Rust</i><br><br>
      ✓ Protocole distant personnalisé à connexion instantanée<br>
      ✓ Streaming adaptatif optimisé pour la stabilité Wi-Fi<br>
      ✓ Support des manettes de jeu<br>  
      ✓ Gestion d'affichage virtuel sans tête<br>
      ✓ Passage à travers de contrôleur de manette à faible latence<br>
      ✓ Accès client navigateur web (aucune installation requise)<br>
      ✓ Accès pré-login Windows (entrée sécurisée des identifiants)
    </td>
    <td width="33%" valign="top">
      <code>+ FONCTIONNALITÉS DE BASE</code> <code>+ FONCTIONNALITÉS GRATUITES</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Flux de travail professionnels</i><br><br>
      ✓ Précision des couleurs chroma 4:4:4 sans perte<br>
      ✓ Passage à travers de périphériques USB bruts<br>
      ✓ Support des tablettes Wacom avec pression & inclinaison<br><br>
    </td>
  </tr>
</table>

## Démarrage rapide

1. **Installez l'Agent** sur la machine que vous souhaitez accéder à distance. Lancez-le — il affichera un jeton de connexion et une adresse Tailscale. Connectez Tailscale si vous avez besoin d'un accès via Internet.

2. **Installez le Client** sur votre station de travail, ordinateur portable ou téléphone.

3. **Ajoutez une connexion** — entrez l'adresse IP ou l'adresse Tailscale affichée dans la fenêtre de l'Agent. C'est tout.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote">
</div>

## Intégration matérielle

<table>
  <tr>
   <td width="50%" valign="top">
      <b>Contrôle BIOS au niveau matériel</b><br>
      USBridge Remote s'intègre nativement avec l'appareil USBridge-KVM 2.0 pour une gestion hors bande, bare-metal avant le démarrage de l'OS.<br><br>
      <a href="https://www.usbridge.io/hardware-agent#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Acheter USBridge-KVM 2.0"></a>
    </td>
    <td width="50%" valign="top">
      <b>Firmware IP-KVM DIY</b><br>
      Déployez le firmware officiel sur un SBC compatible (par exemple, Radxa Zero 3W/3E, Cubie A7) avec une interface de capture USB pour provisionner un nœud KVM personnalisé.<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Obtenir le Firmware"></a>
    </td>
  </tr>
</table>

## Ressources & Licence

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Développement & Feuille de route</b><br>
      Je maintiens un tableau de bord public pour suivre toutes les fonctionnalités prévues, les mises à jour architecturales et les calendriers de publication.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">Voir la feuille de route en direct</a>
    </td>
    <td width="33%" valign="top">
      <b>Liens du projet</b><br>
      <ul>
        <li><a href="https://usbridge.io">Site officiel</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (Tests bêta & Bugs)</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Soutien Patreon</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>Licence (GPLv3)</b><br>
      Ce projet est sous licence <b>GPLv3</b>. Le client multiplateforme incorpore le code de <code>moonlight-common-c</code> (également GPLv3).<br><br>
      <a href="LICENSE">Voir le fichier de licence</a>
    </td>
  </tr>
</table>