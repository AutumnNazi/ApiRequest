[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('amd64', 'arm64')]
    [string]$Architecture,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9A-Za-z][0-9A-Za-z.+-]*$')]
    [string]$AssetVersion,

    [string]$Executable = 'build/bin/ApiRequest.exe',
    [string]$OutputDirectory = 'dist',
    [string]$WixExecutable = 'wix'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$versionParts = @($Version.Split('.') | ForEach-Object { [int]$_ })
if ($versionParts[0] -gt 255 -or $versionParts[1] -gt 255 -or $versionParts[2] -gt 65535) {
    throw "Version '$Version' exceeds Windows Installer limits"
}
$exePath = (Resolve-Path -LiteralPath (Join-Path $repoRoot $Executable)).Path
$wxsPath = (Resolve-Path -LiteralPath (Join-Path $repoRoot 'build/windows/installer/package.wxs')).Path
$iconPath = (Resolve-Path -LiteralPath (Join-Path $repoRoot 'build/windows/icon.ico')).Path
$outputPath = Join-Path $repoRoot $OutputDirectory
New-Item -ItemType Directory -Path $outputPath -Force | Out-Null
$outputPath = (Resolve-Path -LiteralPath $outputPath).Path

$goMetadata = (& go version -m $exePath 2>&1) -join "`n"
if ($LASTEXITCODE -ne 0) {
    throw "Unable to inspect executable architecture: $goMetadata"
}
if ($goMetadata -notmatch "GOARCH=$([regex]::Escape($Architecture))(\s|$)") {
    throw "Expected a $Architecture executable, but go version reported:`n$goMetadata"
}

$settings = switch ($Architecture) {
    'amd64' {
        @{
            Label = 'Amd64'
            WixArch = 'x64'
            MsiPlatform = 'x64'
            UpgradeCode = 'abecc907-e76b-4dfe-b194-f9e249040058'
        }
    }
    'arm64' {
        @{
            Label = 'Arm64'
            WixArch = 'arm64'
            MsiPlatform = 'Arm64'
            UpgradeCode = '249a7fcf-8e7d-415a-ad07-fe8dd7f8e0e8'
        }
    }
}

$assetBase = "ApiRequest-$AssetVersion-Windows-$($settings.Label)"
$portableExe = Join-Path $outputPath "$assetBase-Portable.exe"
$portableZip = Join-Path $outputPath "$assetBase-Portable.zip"
$installerMsi = Join-Path $outputPath "$assetBase-Installer.msi"

Copy-Item -LiteralPath $exePath -Destination $portableExe -Force
Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
if (Test-Path -LiteralPath $portableZip) {
    Remove-Item -LiteralPath $portableZip -Force
}
$writeArchive = [IO.Compression.ZipFile]::Open($portableZip, [IO.Compression.ZipArchiveMode]::Create)
try {
    [void][IO.Compression.ZipFileExtensions]::CreateEntryFromFile(
        $writeArchive,
        $exePath,
        'ApiRequest.exe',
        [IO.Compression.CompressionLevel]::Optimal
    )
} finally {
    $writeArchive.Dispose()
}

$wixCommand = Get-Command $WixExecutable -ErrorAction Stop
$wixArgs = @(
    'build', $wxsPath,
    '-arch', $settings.WixArch,
    '-d', "AppVersion=$Version",
    '-d', "AppExecutable=$exePath",
    '-d', "AppIcon=$iconPath",
    '-d', "UpgradeCode=$($settings.UpgradeCode)",
    '-pdbtype', 'none',
    '-out', $installerMsi
)
& $wixCommand.Source @wixArgs
if ($LASTEXITCODE -ne 0) {
    throw "WiX failed with exit code $LASTEXITCODE"
}

$archive = [IO.Compression.ZipFile]::OpenRead($portableZip)
try {
    $entries = @($archive.Entries)
    $entryNames = @($entries | ForEach-Object FullName)
    if ($entries.Count -ne 1 -or $entries[0].FullName -ne 'ApiRequest.exe') {
        throw "Portable ZIP must contain only ApiRequest.exe; found: $($entryNames -join ', ')"
    }
    $entryStream = $entries[0].Open()
    $sha256 = [Security.Cryptography.SHA256]::Create()
    try {
        $zipHash = [BitConverter]::ToString($sha256.ComputeHash($entryStream)).Replace('-', '')
    } finally {
        $sha256.Dispose()
        $entryStream.Dispose()
    }
} finally {
    $archive.Dispose()
}

$sourceHash = (Get-FileHash -LiteralPath $exePath -Algorithm SHA256).Hash
$portableHash = (Get-FileHash -LiteralPath $portableExe -Algorithm SHA256).Hash
if ($portableHash -ne $sourceHash -or $zipHash -ne $sourceHash) {
    throw 'Portable EXE or ZIP content differs from the source executable'
}

function Get-MsiScalar {
    param($Database, [string]$Query)

    $view = $null
    $record = $null
    try {
        $view = $Database.OpenView($Query)
        [void]$view.Execute()
        $record = $view.Fetch()
        if ($null -eq $record) {
            throw "MSI query returned no rows: $Query"
        }
        return [string]($record.StringData(1))
    } finally {
        if ($null -ne $record) {
            [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($record)
        }
        if ($null -ne $view) {
            [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view)
        }
    }
}

function Get-MsiRowCount {
    param($Database, [string]$Query)

    $view = $null
    $record = $null
    $count = 0
    try {
        $view = $Database.OpenView($Query)
        [void]$view.Execute()
        while ($null -ne ($record = $view.Fetch())) {
            $count++
            [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($record)
            $record = $null
        }
        return $count
    } finally {
        if ($null -ne $record) {
            [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($record)
        }
        if ($null -ne $view) {
            [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view)
        }
    }
}

function Get-WebView2LaunchCondition {
    param($Database)

    $view = $null
    $record = $null
    try {
        $view = $Database.OpenView("SELECT ``Condition``, ``Description`` FROM ``LaunchCondition``")
        [void]$view.Execute()
        while ($null -ne ($record = $view.Fetch())) {
            $condition = [string]($record.StringData(1))
            $description = [string]($record.StringData(2))
            [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($record)
            $record = $null
            if ($condition -match 'WEBVIEW2') {
                return [pscustomobject]@{
                    Condition = $condition
                    Description = $description
                }
            }
        }
        throw 'MSI WebView2 launch condition was not found'
    } finally {
        if ($null -ne $record) {
            [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($record)
        }
        if ($null -ne $view) {
            [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view)
        }
    }
}

$windowsInstaller = $null
$database = $null
$summary = $null
try {
    $windowsInstaller = New-Object -ComObject WindowsInstaller.Installer
    $database = $windowsInstaller.OpenDatabase($installerMsi, 0)
    $summary = $database.SummaryInformation(0)
    $template = [string]$summary.Property(7)
    if ($template -notmatch "(?i)^$([regex]::Escape($settings.MsiPlatform));") {
        throw "MSI template '$template' does not match $($settings.MsiPlatform)"
    }
    $productVersion = Get-MsiScalar $database "SELECT ``Value`` FROM ``Property`` WHERE ``Property`` = 'ProductVersion'"
    $actualUpgradeCode = Get-MsiScalar $database "SELECT ``Value`` FROM ``Property`` WHERE ``Property`` = 'UpgradeCode'"
    $allUsers = Get-MsiScalar $database "SELECT ``Value`` FROM ``Property`` WHERE ``Property`` = 'ALLUSERS'"
    $fileName = Get-MsiScalar $database "SELECT ``FileName`` FROM ``File``"
    $webView2Launch = Get-WebView2LaunchCondition $database
    $shortcutCount = Get-MsiRowCount $database "SELECT ``Shortcut`` FROM ``Shortcut``"
    $appSearchCount = Get-MsiRowCount $database "SELECT ``Property`` FROM ``AppSearch``"
    $registrySearchCount = Get-MsiRowCount $database "SELECT ``Signature_`` FROM ``RegLocator``"

    if ($productVersion -ne $Version) {
        throw "MSI ProductVersion '$productVersion' does not match $Version"
    }
    if ($actualUpgradeCode.Trim('{}') -ne $settings.UpgradeCode) {
        throw "MSI UpgradeCode '$actualUpgradeCode' does not match $($settings.UpgradeCode)"
    }
    if ($allUsers -ne '1') {
        throw "MSI ALLUSERS must be 1, found '$allUsers'"
    }
    if ($fileName -notmatch '(?i)(^|\|)ApiRequest\.exe$') {
        throw "MSI File table does not contain ApiRequest.exe, found '$fileName'"
    }
    if ($shortcutCount -ne 2) {
        throw "MSI must contain two shortcuts, found $shortcutCount"
    }
    if ($appSearchCount -ne 3 -or $registrySearchCount -ne 3) {
        throw "MSI must contain three WebView2 registry searches; found AppSearch=$appSearchCount, RegLocator=$registrySearchCount"
    }
    if ($webView2Launch.Condition -notmatch 'ACTION\s*=\s*"ADMIN"' -or $webView2Launch.Description -notmatch 'WebView2 Runtime') {
        throw 'MSI WebView2 launch condition is incomplete'
    }
} finally {
    if ($null -ne $summary) {
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($summary)
    }
    if ($null -ne $database) {
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($database)
    }
    if ($null -ne $windowsInstaller) {
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($windowsInstaller)
    }
}

$extractDirectory = Join-Path ([IO.Path]::GetTempPath()) "ApiRequest-msi-$([Guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $extractDirectory -Force | Out-Null
try {
    $msiLog = Join-Path $extractDirectory 'msiexec.log'
    $msiArguments = @(
        '/a', "`"$installerMsi`"",
        '/qn',
        '/norestart',
        "TARGETDIR=`"$extractDirectory`"",
        '/l*v', "`"$msiLog`""
    )
    $msiProcess = Start-Process `
        -FilePath (Join-Path $env:SystemRoot 'System32/msiexec.exe') `
        -ArgumentList $msiArguments `
        -Wait `
        -PassThru `
        -NoNewWindow
    if ($msiProcess.ExitCode -ne 0) {
        $logTail = if (Test-Path -LiteralPath $msiLog) {
            (Get-Content -LiteralPath $msiLog -Tail 40) -join "`n"
        } else {
            'MSI log was not created.'
        }
        throw "MSI administrative extraction failed with exit code $($msiProcess.ExitCode)`n$logTail"
    }
    $extractedExecutables = @(Get-ChildItem -LiteralPath $extractDirectory -Recurse -File -Filter 'ApiRequest.exe')
    if ($extractedExecutables.Count -ne 1) {
        throw "MSI must extract exactly one ApiRequest.exe, found $($extractedExecutables.Count)"
    }
    $extractedHash = (Get-FileHash -LiteralPath $extractedExecutables[0].FullName -Algorithm SHA256).Hash
    if ($extractedHash -ne $sourceHash) {
        throw 'MSI embedded executable differs from the source executable'
    }
} finally {
    Remove-Item -LiteralPath $extractDirectory -Recurse -Force -ErrorAction SilentlyContinue
}

Get-Item -LiteralPath $portableExe, $portableZip, $installerMsi |
    Select-Object Name, Length, FullName
