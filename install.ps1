$ErrorActionPreference = "Stop"

$repo = "asynkron/Asynkron.QuickDup"
$installDir = "$env:LOCALAPPDATA\quickdup"

# Detect architecture
$arch = if ([Environment]::Is64BitOperatingSystem) {
    if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
} else {
    Write-Error "32-bit systems are not supported"
    exit 1
}

# Get latest version
$release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest"
$version = $release.tag_name
$versionNum = $version.TrimStart('v')

Write-Host "Installing quickdup $version for windows/$arch..."

# Download
$url = "https://github.com/$repo/releases/download/$version/quickdup_${versionNum}_windows_${arch}.tar.gz"
$tmpFile = Join-Path $env:TEMP "quickdup.tar.gz"
$tmpDir = Join-Path $env:TEMP "quickdup_extract"

Invoke-WebRequest -Uri $url -OutFile $tmpFile

# Extract (requires tar, available in Windows 10+)
New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
tar -xzf $tmpFile -C $tmpDir

# Install
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
Move-Item -Force "$tmpDir\quickdup.exe" "$installDir\quickdup.exe"

# Cleanup
Remove-Item -Force $tmpFile
Remove-Item -Recurse -Force $tmpDir

# Add to PATH if not already there
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$installDir", "User")
    $env:Path = "$env:Path;$installDir"
    Write-Host "Added $installDir to PATH"
}

Write-Host "quickdup installed to $installDir\quickdup.exe"
Write-Host "Run 'quickdup --help' to get started"
Write-Host ""
Write-Host "Note: Restart your terminal for PATH changes to take effect"
