; NerdBackup Agent — Inno Setup Installer Script
; https://jrsoftware.org/ishelp/

#ifndef MyAppVersion
  #define MyAppVersion "0.0.0"
#endif

#define MyAppName "NerdBackup Agent"
#define MyAppPublisher "NerdBackup"
#define MyAppURL "https://nerdbackup.com"
#define MyAppExeName "nerdbackup-agent.exe"
#define ServiceName "NerdBackupAgent"

[Setup]
AppId={{B7E3D4A1-9F2C-4E8B-A1D6-5C7F8E9B0A2D}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}/docs
DefaultDirName={autopf}\NerdBackup
DefaultGroupName={#MyAppName}
LicenseFile=..\LICENSE
OutputBaseFilename=nerdbackup-agent-{#MyAppVersion}-windows-setup
Compression=lzma2
SolidCompression=yes
PrivilegesRequired=admin
ChangesEnvironment=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayIcon={app}\{#MyAppExeName}
WizardStyle=modern
WizardSizePercent=100

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
Source: "dist\nerdbackup-agent.exe"; DestDir: "{app}"; Flags: ignoreversion

[Dirs]
Name: "{commonappdata}\NerdBackup"; Permissions: everyone-modify

[Icons]
Name: "{group}\NerdBackup Agent Status"; Filename: "{app}\{#MyAppExeName}"; Parameters: "status"
Name: "{group}\NerdBackup Dashboard"; Filename: "{#MyAppURL}/dashboard"
Name: "{group}\Uninstall {#MyAppName}"; Filename: "{uninstallexe}"

[Registry]
; Add to system PATH
Root: HKLM; Subkey: "SYSTEM\CurrentControlSet\Control\Session Manager\Environment"; \
  ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; \
  Check: NeedsAddPath(ExpandConstant('{app}'))

; NO [Run] section — all service operations handled in [Code]
; NO [UninstallRun] section — all cleanup handled in [Code]

[UninstallDelete]
Type: filesandordirs; Name: "{commonappdata}\NerdBackup"

[Code]
var
  ActivationPage: TInputQueryWizardPage;
  InstallToken: String;
  ApiUrl: String;

procedure InitializeWizard;
begin
  InstallToken := ExpandConstant('{param:INSTALL_TOKEN|}');
  ApiUrl := ExpandConstant('{param:API_URL|https://nerdbackup.com}');

  if InstallToken = '' then
  begin
    ActivationPage := CreateInputQueryPage(wpSelectDir,
      'NerdBackup Activation',
      'Enter your activation code to link this agent to your account.',
      'Get your activation code from https://nerdbackup.com/dashboard/agents' + #13#10 + #13#10 +
      'You can leave this blank and configure it later with:' + #13#10 +
      '  nerdbackup-agent init --api-key YOUR_API_KEY');
    ActivationPage.Add('Activation Code:', False);
  end;
end;

function GetInstallToken(Param: String): String;
begin
  if InstallToken <> '' then
    Result := InstallToken
  else if Assigned(ActivationPage) then
    Result := ActivationPage.Values[0]
  else
    Result := '';
end;

function GetApiUrl(Param: String): String;
begin
  Result := ApiUrl;
end;

function HasInstallToken: Boolean;
begin
  Result := GetInstallToken('') <> '';
end;

function NeedsAddPath(Param: string): boolean;
var
  OrigPath: string;
begin
  if not RegQueryStringValue(HKEY_LOCAL_MACHINE,
    'SYSTEM\CurrentControlSet\Control\Session Manager\Environment',
    'Path', OrigPath)
  then begin
    Result := True;
    exit;
  end;
  Result := Pos(';' + Uppercase(Param) + ';', ';' + Uppercase(OrigPath) + ';') = 0;
end;

// ── Post-Install: register agent, install + start service ──
procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
  Token: String;
begin
  if CurStep = ssPostInstall then
  begin
    // 1. Register agent with install token (if provided)
    Token := GetInstallToken('');
    if Token <> '' then
    begin
      Exec(ExpandConstant('{app}\{#MyAppExeName}'),
        'init --install-token "' + Token + '" --api-url "' + GetApiUrl('') + '"',
        '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
    end;

    // 2. Install Windows Service
    Exec(ExpandConstant('{app}\{#MyAppExeName}'), 'service install',
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode);

    // 3. Start service via sc.exe (non-blocking — don't wait for service to fully start)
    Exec(ExpandConstant('{sys}\sc.exe'), 'start {#ServiceName}',
      '', SW_HIDE, ewNoWait, ResultCode);
  end;
end;

procedure RemoveFromPath(AppDir: string);
var
  OrigPath: string;
begin
  if RegQueryStringValue(HKEY_LOCAL_MACHINE,
    'SYSTEM\CurrentControlSet\Control\Session Manager\Environment',
    'Path', OrigPath)
  then begin
    StringChangeEx(OrigPath, ';' + AppDir, '', True);
    StringChangeEx(OrigPath, AppDir + ';', '', True);
    StringChangeEx(OrigPath, AppDir, '', True);
    RegWriteStringValue(HKEY_LOCAL_MACHINE,
      'SYSTEM\CurrentControlSet\Control\Session Manager\Environment',
      'Path', OrigPath);
  end;
end;

// ── Pre-Uninstall: stop service, remove service, deregister, kill process ──
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  ResultCode: Integer;
begin
  if CurUninstallStep = usUninstall then
  begin
    // 1. Stop the service via sc.exe
    Exec(ExpandConstant('{sys}\sc.exe'), 'stop {#ServiceName}',
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
    Sleep(3000);

    // 2. Delete the service registration
    Exec(ExpandConstant('{sys}\sc.exe'), 'delete {#ServiceName}',
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
    Sleep(2000);

    // 3. Kill any remaining agent process (so files can be deleted)
    Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM {#MyAppExeName}',
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode);

    // 4. Kill any running restic process (may still be mid-backup)
    Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM restic.exe',
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
    Sleep(1000);

    // 4. Deregister from NerdBackup API (best effort)
    Exec(ExpandConstant('{app}\{#MyAppExeName}'), 'uninstall',
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  end;

  if CurUninstallStep = usPostUninstall then
  begin
    RemoveFromPath(ExpandConstant('{app}'));
  end;
end;
