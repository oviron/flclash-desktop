; The Windows installer: FlClash for the current user, no administrator rights.
; The release workflow builds it from dist\windows\FlClash:
;
;   iscc /DVersion=0.2.0 /DFileVersion=0.2.0 /DArch=x64 /DSource=...\dist\windows\FlClash /DOutput=out windows\installer.iss
;
; The helper for the tunnel is not the installer's: the app installs it when
; the tunnel is first turned on, and "flclashctl helper uninstall", run as
; administrator, removes it.

#ifndef Version
  #define Version "0.0.0-dev"
#endif
#ifndef FileVersion
  #define FileVersion "0.0.0"
#endif
#ifndef Arch
  #define Arch "x64"
#endif
#ifndef Source
  #define Source "..\dist\windows\FlClash"
#endif
#ifndef Output
  #define Output "..\dist"
#endif

[Setup]
AppId={{6F1E3C2A-6B8D-4C55-9C1F-3A7E2D9B5F41}
AppName=FlClash
AppVersion={#Version}
AppVerName=FlClash {#Version}
AppPublisher=oviron
AppPublisherURL=https://github.com/oviron/flclash-desktop
AppSupportURL=https://github.com/oviron/flclash-desktop/issues
VersionInfoVersion={#FileVersion}
LicenseFile=..\LICENSE
DefaultDirName={localappdata}\Programs\FlClash
DisableProgramGroupPage=yes
DisableDirPage=auto
PrivilegesRequired=lowest
#if Arch == "arm64"
ArchitecturesAllowed=arm64
ArchitecturesInstallIn64BitMode=arm64
#else
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
#endif
MinVersion=10.0.19041
SetupIconFile=FlClash.ico
UninstallDisplayIcon={app}\FlClash.exe
UninstallDisplayName=FlClash
OutputDir={#Output}
OutputBaseFilename=FlClash-{#Version}-windows-{#Arch}-setup
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
; FlClash.exe restores the system proxy when it is asked to close.
CloseApplications=yes
RestartApplications=no

[Languages]
Name: "en"; MessagesFile: "compiler:Default.isl"
Name: "ru"; MessagesFile: "compiler:Languages\Russian.isl"

[InstallDelete]
; The window is replaced whole: files of an earlier window, such as the
; runtime the WinUI one carried, would stay behind otherwise.
Type: filesandordirs; Name: "{app}\window"

[Files]
Source: "{#Source}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{userprograms}\FlClash"; Filename: "{app}\FlClash.exe"

[Run]
Filename: "{app}\FlClash.exe"; Description: "{cm:LaunchProgram,FlClash}"; Flags: nowait postinstall skipifsilent

[Code]
const RunKey = 'Software\Microsoft\Windows\CurrentVersion\Run';

{ Start with Windows is the app's own setting. The uninstaller clears it when
  it starts this copy, not another one, such as one unpacked from the zip. }
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Command: String;
begin
  if (CurUninstallStep = usPostUninstall) and RegQueryStringValue(HKCU, RunKey, 'FlClash', Command)
    and (Pos(Lowercase(ExpandConstant('{app}\FlClash.exe')), Lowercase(Command)) > 0) then
    RegDeleteValue(HKCU, RunKey, 'FlClash');
end;
