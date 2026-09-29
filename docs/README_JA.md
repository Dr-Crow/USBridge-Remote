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
クライアントは制御インターフェースであり、ワークステーションやラップトップにインストールされるか（またはブラウザで直接実行されます）。接続、ライブリモートデスクトップ、仮想デバイスのパススルー、スナップショットレジストリを管理します。

| アーキテクチャ | Windows | macOS | Linux | Android | iOS | ウェブブラウザ |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [ダウンロード](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [ダウンロード](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [アプリを開く](https://web.usbridge.io) |
| **ARM64** | — | [ダウンロード](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [アプリを開く](https://web.usbridge.io) |

Playストアアカウントなしで直接APKを希望しますか？自己更新ビルドも[最新リリース](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest)に公開されています。

🌐 **ゼロインストールウェブクライアント**: インストールは不要です。接続するには[web.usbridge.io](https://web.usbridge.io)を開くだけです。 *(注: ウェブクライアントは、ブラウザのセキュリティサンドボックスとWebRTCの制約により、一部の機能とパフォーマンスに制限があります。完全な体験を得るには、ネイティブアプリを使用してください。)* 新しく起動したエージェントでは、最初に到達可能になるまで最大1分かかる場合があります（またはネットワーク変更後） — 信頼されたHTTPS証明書を自分自身にプロビジョニングするためです — エージェントの**ステータス → 証明書**行、または[エージェントのドキュメント](agent/docs/README.md#platform-notes-from-the-top-level-readme)を参照してください。

## エージェント

エージェントはターゲットマシン上で動作します — リモートでアクセスしたいサーバーまたはPCです。画面キャプチャ、入力注入、Tailscaleネットワーキングを処理します。

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
      ✓ 内蔵オープンソースのSunshineストリーマー<br>
      ✓ 低遅延のデスクトップ＆ゲームアクセス<br>
      ✓ 双方向ファイル＆テキストクリップボード<br>
      ✓ ゲームパッドサポート<br>      
      ✓ マルチモニター表示切替<br>
      ✓ 統合されたTailscale P2Pネットワーキング<br>
      ✓ ネイティブWaylandサポート（プロンプトなしのキャプチャ）
    </td>
    <td width="33%" valign="top">
      <code>+ 基本機能</code><br><br>
      <b>USBRIDGE 無料</b><br>
      <i>カスタムリモートエンジン</i><br><br>
      ✓ インスタント接続カスタムリモートプロトコル<br>
      ✓ Wi-Fiの安定性に最適化された適応ストリーミング<br>
      ✓ ヘッドレス仮想ディスプレイ管理<br>
      ✓ 低遅延ゲームパッドコントローラパススルー<br>
      ✓ ウェブブラウザクライアントアクセス（インストール不要）<br>
      ✓ Windowsのログイン前アクセス（安全な資格情報入力）
    </td>
    <td width="33%" valign="top">
      <code>+ 基本機能</code> <code>+ 無料機能</code><br><br>
      <b>USBRIDGE プロ</b><br>
      <i>プロフェッショナルワークフロー</i><br><br>
      ✓ ロスレス4:4:4クロマカラー精度<br>
      ✓ 生のUSB周辺機器デバイスパススルー<br>
      ✓ 圧力と傾きに対応したグラフィックタブレットサポート<br><br>
    </td>
  </tr>
</table>

## クイックスタート

1. **エージェントをインストール**します。リモートでアクセスしたいマシンにインストールします。起動すると、接続トークンとTailscaleアドレスが表示されます。インターネット経由でアクセスする必要がある場合は、Tailscaleに接続してください。

2. **クライアントをインストール**します。ワークステーション、ラップトップ、または電話にインストールします。

3. **接続を追加**します — エージェントウィンドウに表示されているIPまたはTailscaleアドレスを入力します。それだけです。

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote">
</div>

## ハードウェア統合

<table>
  <tr>
   <td width="50%" valign="top">
      <b>ハードウェアレベルのBIOS制御</b><br>
      USBridge Remoteは、OSブート前のアウトオブバンド、ベアメタル管理のためにUSBridge-KVM 2.0アプライアンスとネイティブに統合されています。<br><br>
      <a href="https://www.usbridge.io/hardware-agent#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Buy USBridge-KVM 2.0"></a>
    </td>
    <td width="50%" valign="top">
      <b>DIY IP-KVMファームウェア</b><br>
      USBキャプチャインターフェースを持つ互換性のあるSBC（例：Radxa Zero 3W/3E、Cubie A7）に公式ファームウェアを展開して、カスタムKVMノードをプロビジョニングします。<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Get the Firmware"></a>
    </td>
  </tr>
</table>

## リソースとライセンス

<table>
  <tr>
    <td width="33%" valign="top">
      <b>開発とロードマップ</b><br>
      計画されているすべての機能、アーキテクチャの更新、およびリリーススケジュールを追跡するための公開ダッシュボードを維持しています。<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">ライブロードマップを見る</a>
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
      <b>ライセンス (GPLv3)</b><br>
      このプロジェクトは<b>GPLv3</b>の下でライセンスされています。マルチプラットフォームクライアントは<code>moonlight-common-c</code>（同じくGPLv3）からのコードベースを組み込んでいます。<br><br>
      <a href="LICENSE">LICENSEファイルを見る</a>
    </td>
  </tr>
</table>