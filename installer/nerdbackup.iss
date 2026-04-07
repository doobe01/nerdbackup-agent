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
SetupIconFile=..\assets\icon.ico
UninstallDisplayIcon={app}\{#MyAppExeName}
WizardStyle=modern
WizardSizePercent=100

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
Source: "dist\nerdbackup-agent.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; DestName: "LICENSE.txt"; Flags: ignoreversion

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

[Run]
; Register agent with install token (if provided)
Filename: "{app}\{#MyAppExeName}"; Parameters: "init --install-token ""{code:GetInstallToken}"" --api-url ""{code:GetApiUrl}"""; \
  StatusMsg: "Registering agent with NerdBackup..."; Flags: runhidden waituntilterminated; \
  Check: HasInstallToken
; Install and start Windows Service (nowait — service starts in background)
Filename: "{app}\{#MyAppExeName}"; Parameters: "service install"; \
  StatusMsg: "Installing Windows service..."; Flags: runhidden waituntilterminated
Filename: "{app}\{#MyAppExeName}"; Parameters: "service start"; \
  StatusMsg: "Starting NerdBackup Agent..."; Flags: runhidden nowait

[UninstallRun]
; Stop and remove service, deregister from API
Filename: "{app}\{#MyAppExeName}"; Parameters: "service stop"; \
  Flags: runhidden waituntilterminated
Filename: "{app}\{#MyAppExeName}"; Parameters: "service uninstall"; \
  Flags: runhidden waituntilterminated
Filename: "{app}\{#MyAppExeName}"; Parameters: "uninstall"; \
  Flags: runhidden nowait

[UninstallDelete]
Type: filesandordirs; Name: "{commonappdata}\NerdBackup"

[Code]
var
  ActivationPage: TInputQueryWizardPage;
  InstallToken: String;
  ApiUrl: String;

procedure InitializeWizard;
begin
  // Check for command-line parameters (silent install)
  InstallToken := ExpandConstant('{param:INSTALL_TOKEN|}');
  ApiUrl := ExpandConstant('{param:API_URL|https://nerdbackup.com}');

  // Only show activation page if no token was passed via command line
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

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  OrigPath, AppDir: string;
begin
  // Remove from PATH on uninstall
  if CurUninstallStep = usPostUninstall then
  begin
    AppDir := ExpandConstant('{app}');
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
end;
