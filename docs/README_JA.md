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

**USBridge Remote** は、リモートマシンを管理するための統合された高性能クライアントです。**ハードウェアレベルのBIOSアクセス**（USBridge KVMデバイスを介して）と**ソフトウェアベースのリモートデスクトップ**を1つの洗練されたインターフェースに統合するように設計されています。

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote">
</div>


## ダウンロード

### クライアント
クライアントは制御インターフェースであり、ワークステーションまたはラップトップにインストールされます（またはブラウザで直接実行できます）。接続、ライブリモートデスクトップ、仮想デバイスのパススルー、スナップショットレジストリを管理します。

| アーキテクチャ | Windows | macOS | Linux | Android | iOS | Webブラウザ |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [ダウンロード](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [ダウンロード](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [アプリを開く](https://web.usbridge.io) |
| **ARM64** | — | [ダウンロード](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [アプリを開く](https://web.usbridge.io) |

Play Storeアカウントなしで直接APKを希望しますか？自己更新ビルドも[最新リリース](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest)に公開されています。

🌐 **ゼロインストールWebクライアント**: インストールは不要です。接続するには[web.usbridge.io](https://web.usbridge.io)を開くだけです。 *(注: Webクライアントは、ブラウザのセキュリティサンドボックスとWebRTCの制約により、一部の機能とパフォーマンスに制限があります。完全な体験を得るには、ネイティブアプリを使用してください。)* 新しく起動したエージェントでは、信頼されたHTTPS証明書をプロビジョニングするため、最初の接続（またはネットワーク変更後）に最大1分かかることがあります — エージェントの**ステータス → 証明書**行、または[エージェントドキュメント](agent/docs/README.md#platform-notes-from-the-top-level-readme)を参照してください。

## エージェント

エージェントは、リモートでアクセスしたいターゲットマシン（サーバーまたはPC）上で実行されます。画面キャプチャ、入力注入、およびTailscaleネットワーキングを処理します。

<table>
  <tr>
    <!-- 左列: ダウンロードテーブル -->
    <td valign="middle">
      <table>
        <tr>
          <th>アーキテクチャ</th>
          <th>Windows</th>
          <th>macOS</th>
          <th>Linux</th>
        </tr>
        <tr>
          <td><b>x86_64</b></td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">ダウンロード</a></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">ダウンロード</a></td>
        </tr>
        <tr>
          <td><b>ARM64</b></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">ダウンロード</a></td>
          <td>—</td>
        </tr>
      </table>
    </td>
    <!-- 右列: 画像 -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="USBridge Agent Interface" width="100%">
    </td>
  </tr>
</table>

## 機能

<table>
  <tr>
    <td width="33%" valign="top">
      <code>基本機能</code><br><br>
      <b>SUNSHINE オープンソース</b><br>
      <i>標準ゲームストリーミング</i><br><br>
      ✓ 組み込みのオープンソースSunshineストリーマー<br>
      ✓ 低遅延のデスクトップ＆ゲームアクセス<br>
      ✓ 双方向ファイル＆テキストクリップボード<br>    
      ✓ マルチモニタ表示切り替え<br>
      ✓ 統合されたTailscale P2Pネットワーキング<br>
      ✓ ネイティブWaylandサポート（プロンプトなしキャプチャ）
    </td>
    <td width="33%" valign="top">
      <code>+ 基本機能</code><br><br>
      <b>USBRIDGE 無料</b><br>
      <i>Rustベースのストリームエンジン</i><br><br>
      ✓ 即時接続カスタムリモートプロトコル<br>
      ✓ 生USB周辺機器のパススルー<br>
      ✓ Wi-Fiの安定性に最適化された適応ストリーミング<br>
      ✓ HDR10<br>
      ✓ ヘッドレス仮想ディスプレイ管理<br>
      ✓ 低遅延ゲームパッドコントローラのパススルー<br>
      ✓ Webブラウザクライアントアクセス（インストール不要）<br>
      ✓ Windowsのログイン前アクセス（安全な資格情報入力）
    </td>
    <td width="33%" valign="top">
      <code>+ 基本機能</code> <code>+ 無料機能</code><br><br>
      <b>USBRIDGE プロ</b><br>
      <i>プロフェッショナルワークフロー</i><br><br>
      ✓ ロスレス4:4:4クロマカラー精度<br>
      ✓ 圧力と傾きに対応したWacomタブレットサポート<br><br>
    </td>
  </tr>
</table>

## クイックスタート

1. **エージェントをインストール**します。リモートでアクセスしたいマシンにインストールします。起動すると接続トークンとTailscaleアドレスが表示されます。インターネット経由でアクセスする必要がある場合は、Tailscaleを接続してください。

2. **クライアントをインストール**します。ワークステーション、ラップトップ、または電話にインストールします。

3. **接続を追加**します — エージェントウィンドウに表示されているIPまたはTailscaleアドレスを入力します。それだけです。

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote">
</div>

## ハードウェア統合

<table>
  <tr>
   <td width="33%" valign="top">
      <b>ハードウェアレベルのBIOS制御</b><br>
      USBridge Remoteは、OSブート前のアウトオブバンド、ベアメタル管理のためにUSBridge-KVM 2.0アプライアンスとネイティブに統合されています。<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Buy USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>DIY IP-KVMファームウェア</b><br>
      USBキャプチャインターフェースを備えた互換性のあるSBC（例：Radxa Zero 3W/3E、Cubie A7）に公式ファームウェアを展開してカスタムKVMノードをプロビジョニングします。<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Get the Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>USB/IPハードウェアドングル（ESP32-S3）</b><br>
      ソフトウェアUSBパススルーはmacOSで壁にぶつかります：接続するためのVHCIドライバがないため、リモートWacomタブレット（または他のUSBデバイス）を実際のハードウェアとして提示することができません。このドングルはMacに接続し、他の場所でソフトウェアで行う<code>vhci-hcd</code>の機能をハードウェアで実行します — エクスポートされたデバイスは独自のVID/PIDで列挙され、macOSのドライバによってバインドされます。<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="esp32-acm firmware & docs"></a>
    </td>
  </tr>
</table>

## リソースとライセンス

<table>
  <tr>
    <td width="33%" valign="top">
      <b>開発とロードマップ</b><br>
      計画されているすべての機能、アーキテクチャの更新、リリーススケジュールを追跡するための公開ダッシュボードを維持しています。<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">ライブロードマップを表示</a>
    </td>
    <td width="33%" valign="top">
      <b>プロジェクトリンク</b><br>
      <ul>
        <li><a href="https://usbridge.io">公式ウェブサイト</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord（ベータテスト＆バグ）</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Patreonサポート</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>ライセンス（GPLv3）</b><br>
      このプロジェクトは<b>GPLv3</b>の下でライセンスされています。マルチプラットフォームクライアントは<code>moonlight-common-c</code>（同じくGPLv3）からのコードベースを組み込んでいます。<br><br>
      <a href="LICENSE">LICENSEファイルを表示</a>
    </td>
  </tr>
</table>