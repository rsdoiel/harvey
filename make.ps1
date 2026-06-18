param (
    [string]$action = "build"
)

# List of programs to build (each must have a matching cmd/<name>/ directory)
$programs = @("harvey")

$jsonContent = Get-Content -Raw -Path "codemeta.json" | ConvertFrom-Json
$projectName = $jsonContent.name
$versionNo = $jsonContent.version

function Make-Man {
    $markdownFiles = Get-ChildItem -File *.1.md
    foreach ($file in $markdownFiles) {
        $manName = [System.IO.Path]::GetFileNameWithoutExtension($file.Name)

        if (-not (Test-Path -Path man\man1)) {
            New-Item -ItemType Directory -Path man\man1 | Out-Null
        }

        Write-Host "Rendering $file as man\man1\$manName"
        pandoc -f Markdown -t man -o man\man1\$manName -s $file
    }
}

function Build-It {
    param (
        [string]$OutPath,
        [string]$SourcePath,
        [string]$TargetOS,
        [string]$TargetArch
    )
    if ([string]::IsNullOrEmpty($OutPath)) {
        throw "missing required output path"
    }
    if ([string]::IsNullOrEmpty($SourcePath)) {
        throw "missing required go source path"
    }

    # Default to host OS if not specified
    if ([string]::IsNullOrEmpty($TargetOS)) {
        if ($IsWindows) {
            $TargetOS = "windows"
        }
        elseif ($IsMacOS) {
            $TargetOS = "darwin"
        }
        elseif ($IsLinux) {
            $TargetOS = "linux"
        }
        else {
            throw "Unsupported host OS."
        }
    }

    # Default to host architecture if not specified
    if ([string]::IsNullOrEmpty($TargetArch)) {
        $arch = $env:PROCESSOR_ARCHITECTURE
        $archMap = @{
            "AMD64"  = "amd64"
            "x86_64" = "amd64"
            "ARM64"  = "arm64"
            "aarch64" = "arm64"
        }
        if ($archMap.ContainsKey($arch)) {
            $TargetArch = $archMap[$arch]
        }
        else {
            # Fallback for Unix-like systems
            $uname = uname -m
            if ($uname -eq "x86_64") { $TargetArch = "amd64" }
            elseif ($uname -eq "aarch64") { $TargetArch = "arm64" }
            else { throw "Unsupported host architecture: $arch" }
        }
    }

    # Validate supported OS and architecture
    $supportedOS = @("windows", "darwin", "linux")
    $supportedArch = @("amd64", "arm64")

    if ($TargetOS -notin $supportedOS) {
        Write-Error "Unsupported OS: $TargetOS. Use windows, darwin, or linux."
        exit 1
    }
    if ($TargetArch -notin $supportedArch) {
        Write-Error "Unsupported architecture: $TargetArch. Use amd64 or arm64."
        exit 1
    }

    Write-Host "Building $OutPath from $SourcePath for OS: $TargetOS, Architecture: $TargetArch"

    # Set GOOS and GOARCH for cross-compilation
    $env:GOOS = $TargetOS
    $env:GOARCH = $TargetArch
    # Run the Go build command
    go build -o "$OutPath" "$SourcePath"
    if (-not $?) {
        throw "Build failed for $TargetOS/$TargetArch."
    }
}

# Make the man pages
if ($action -eq "man") {
    Make-Man
}

# Default build action
if ($action -eq "build") {
    foreach ($prog in $programs) {
        Write-Host "Building $prog..."
        Build-It -OutPath "bin\$prog.exe" -SourcePath ".\cmd\$prog"
        if ($LASTEXITCODE -ne 0) {
            Write-Error "Build failed for $prog"
            exit $LASTEXITCODE
        }
        Write-Host "Generating help documentation for $prog..."
        & ".\bin\$prog.exe" --help > "$prog.1.md"
    }
    Write-Host "Build and documentation generation complete."
}

# Install action
if ($action -eq "install") {
    $binDir = Join-Path $HOME "bin"
    if (-not (Test-Path $binDir)) {
        Write-Host "Creating $binDir directory..."
        New-Item -ItemType Directory -Path $binDir | Out-Null
    }
    foreach ($prog in $programs) {
        $exePath = "bin\$prog.exe"
        $destPath = Join-Path $binDir "$prog.exe"
        Build-It -OutPath $exePath -SourcePath ".\cmd\$prog"
        Write-Host "Copying $prog.exe to $binDir..."
        Copy-Item -Path $exePath -Destination $destPath -Force
    }

    # Check if $HOME\bin is in PATH
    $pathEnv = [Environment]::GetEnvironmentVariable("PATH", "User")
    if ($pathEnv -notlike "*$binDir*") {
        Write-Host "`n$binDir is not in your PATH. To add it, run the following command:"
        Write-Host "[Environment]::SetEnvironmentVariable('PATH', `'$pathEnv;$binDir`' + ';', 'User')"
        Write-Host "After running the above command, restart your terminal or run:"
        Write-Host "refreshenv"
    } else {
        Write-Host "`n$binDir is already in your PATH."
    }
}

if ($action -eq "release") {
    $releasePath = "dist"
    if (Test-Path -Path $releasePath) {
        Write-Host "Removing stale $releasePath"
        Remove-Item -Path "$releasePath" -Recurse -Force
    }
    New-Item -ItemType Directory -Path $releasePath | Out-Null
    Write-Host "Created directory: $releasePath"

    # Copy in the documentation files
    Copy-Item README.md dist
    Copy-Item INSTALL.md dist
    Copy-Item codemeta.json dist
    Copy-Item LICENSE dist
    Copy-Item *.?.md dist
    Copy-Item -Recurse man dist

    $platforms = @(
        @{ OS = "windows"; Arch = "amd64"; Label = "Windows-x86_64"; Ext = ".exe" },
        @{ OS = "windows"; Arch = "arm64"; Label = "Windows-arm64";  Ext = ".exe" },
        @{ OS = "darwin";  Arch = "amd64"; Label = "macOS-x86_64";   Ext = "" },
        @{ OS = "darwin";  Arch = "arm64"; Label = "macOS-arm64";    Ext = "" },
        @{ OS = "linux";   Arch = "amd64"; Label = "Linux-x86_64";   Ext = "" },
        @{ OS = "linux";   Arch = "arm64"; Label = "Linux-arm64";    Ext = "" }
    )

    foreach ($platform in $platforms) {
        New-Item -ItemType Directory -Path "$releasePath\bin"
        foreach ($prog in $programs) {
            Build-It -OutPath "dist\bin\$prog$($platform.Ext)" `
              -SourcePath ".\cmd\$prog" `
              -TargetOS $platform.OS `
              -TargetArch $platform.Arch
        }
        cd dist
        $filesToZip = @(
            "bin",
            "man",
            "*.md",
            "codemeta.json",
            "INSTALL.md",
            "LICENSE",
            "README.md"
        )
        $targetZip = "$projectName-v$versionNo-$($platform.Label).zip"
        if (Test-Path -Path $targetZip) {
            Remove-Item -Path "$targetZip" -Force
        }
        Compress-Archive -Path $filesToZip -DestinationPath $targetZip -CompressionLevel Optimal
        cd ..
        Remove-Item -Path "dist\bin" -Recurse -Force
    }
    Write-Host "Check the zip files, then do release.ps1 if all is OK"
}
