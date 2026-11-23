; ============================================================================
; quickidc 桌面客户端 · Inno Setup 安装包
;
; 前置构建：
;   cd tools/quickidc-gui
;   npm ci && npm run icon
;   npm run tauri build -- --no-bundle   ; 产出 target/release/quickidc-gui.exe
;   go build -o src-tauri/binaries/quickidc-x86_64-pc-windows-msvc.exe ./cmd/quickidc
;
; 编译：
;   iscc /DAppVersion=1.0.0 packaging/windows/quickidc.iss
; ============================================================================

#ifndef AppVersion
#define AppVersion "1.0.0"
#endif
#define AppName "quickidc"
#define AppPublisher "QuickIDC Team"
#define AppURL "https://github.com/QuickIDC/CloudHost"
#define GuiExe "..\..\tools\quickidc-gui\src-tauri\target\release\quickidc-gui.exe"
#define CliExe "..\..\tools\quickidc-gui\src-tauri\binaries\quickidc-x86_64-pc-windows-msvc.exe"

[Setup]
AppId={{6C1F0B52-9F3A-4B7E-9A21-0D1C5E7F2A44}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}
AppUpdatesURL={#AppURL}
DefaultDirName={autopf}\QuickIDC\quickidc
DefaultGroupName=QuickIDC
DisableProgramGroupPage=yes
OutputDir=..\..\BuildCache\windows
OutputBaseFilename=quickidc-{#AppVersion}-win-x64-setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=admin
UninstallDisplayIcon={app}\quickidc.exe

[Languages]
Name: "chinesesimp"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "附加任务:"; Flags: unchecked
Name: "addtopath"; Description: "将 quickidc CLI 加入系统 PATH"; GroupDescription: "命令行:"; Flags: checkedonce

[Files]
; GUI 主程序
Source: "{#GuiExe}"; DestDir: "{app}"; DestName: "quickidc.exe"; Flags: ignoreversion
; CLI 放到 bin 子目录并加入 PATH，命令名同为 quickidc
Source: "{#CliExe}"; DestDir: "{app}\bin"; DestName: "quickidc.exe"; Flags: ignoreversion

[Icons]
Name: "{group}\quickidc"; Filename: "{app}\quickidc.exe"
Name: "{group}\quickidc CLI"; Filename: "{cmd}"; Parameters: "/k quickidc --help"; IconFilename: "{app}\bin\quickidc.exe"
Name: "{group}\卸载 quickidc"; Filename: "{uninstallexe}"
Name: "{autodesktop}\quickidc"; Filename: "{app}\quickidc.exe"; Tasks: desktopicon

[Registry]
Root: HKLM; Subkey: "SYSTEM\CurrentControlSet\Control\Session Manager\Environment"; \
  ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}\bin"; \
  Tasks: addtopath; Check: NeedsAddPath(ExpandConstant('{app}\bin'))

[Run]
Filename: "{app}\quickidc.exe"; Description: "启动 quickidc"; Flags: nowait postinstall skipifsilent

[Code]
function NeedsAddPath(Param: string): boolean;
var
  OrigPath: string;
begin
  if not RegQueryStringValue(HKLM, 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment', 'Path', OrigPath) then
  begin
    Result := True;
    exit;
  end;
  Result := Pos(';' + Param + ';', ';' + OrigPath + ';') = 0;
end.

[UninstallDelete]
Type: filesandordirs; Name: "{app}"
