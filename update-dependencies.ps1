<#
.SYNOPSIS
Report dependency updates, upgrade dependencies, or tidy dependency state.
.EXAMPLE
./update-dependencies.ps1
.EXAMPLE
./update-dependencies.ps1 -Mode Update -Go -Apply
.EXAMPLE
./update-dependencies.ps1 -Mode Tidy -Apply
#>
param(
    [ValidateSet("Check", "Update", "Tidy")]
    [string]$Mode = "Check",
    [switch]$Go,
    [switch]$Node,
    [switch]$Python,
    [switch]$Apply
)

$ErrorActionPreference = "Stop"
$WorkspaceRoot = $PSScriptRoot

if ($Mode -eq "Check" -and $Apply) {
    throw "-Apply requires -Mode Update or -Mode Tidy."
}
if ($Mode -ne "Check" -and -not $Apply) {
    throw "Modifying dependency state requires -Apply. Run without options to check for updates."
}

if ($Mode -ne "Check") {
    $status = & git -C $WorkspaceRoot status --porcelain --untracked-files=normal
    if ($LASTEXITCODE -ne 0) {
        throw "Could not check the Git working tree. No dependency commands were run."
    }
    if ($status) {
        throw "Commit or stash existing changes before modifying dependencies. No dependency commands were run."
    }
}

if (-not $Go -and -not $Node -and -not $Python) {
    $Go = $Node = $Python = $true
}

function Invoke-DependencyCommand {
    param(
        [string]$Executable,
        [string[]]$CommandArguments,
        [int[]]$AllowedExitCodes = @(0)
    )

    Write-Host "[Runner] $Executable $($CommandArguments -join ' ')" -ForegroundColor Gray
    & $Executable @CommandArguments
    if ($LASTEXITCODE -notin $AllowedExitCodes) {
        throw "$Executable exited with code $LASTEXITCODE."
    }
}

$Projects = @()
if ($Go) {
    foreach ($module in @("cellar", "durable_email", "email_clients", "last_orders")) {
        $Projects += [pscustomobject]@{ Directory = $module; Tool = "go" }
    }
}
if ($Node) {
    foreach ($directory in @("react", "e2e")) {
        $Projects += [pscustomobject]@{ Directory = $directory; Tool = "npm" }
    }
}
if ($Python) {
    $Projects += [pscustomobject]@{ Directory = "firebase_sub"; Tool = "poetry" }
}

$FailedProjects = @()
foreach ($project in $Projects) {
    Write-Host "`n=== $Mode dependencies: $($project.Directory) ===" -ForegroundColor Cyan
    $locationPushed = $false
    try {
        Push-Location (Join-Path $WorkspaceRoot $project.Directory)
        $locationPushed = $true

        switch ($project.Tool) {
            "go" {
                switch ($Mode) {
                    "Check" { Invoke-DependencyCommand "go" @("list", "-mod=readonly", "-m", "-u", "all") }
                    "Update" { Invoke-DependencyCommand "go" @("get", "-u", "./...") }
                    "Tidy" { Invoke-DependencyCommand "go" @("mod", "tidy") }
                }
            }
            "npm" {
                switch ($Mode) {
                    "Check" { Invoke-DependencyCommand "npm" @("outdated") @(0, 1) }
                    "Update" {
                        Invoke-DependencyCommand "npm" @("update", "--ignore-scripts")
                        if ($project.Directory -eq "e2e") {
                            Invoke-DependencyCommand "npm" @("run", "install:browsers", "--ignore-scripts")
                        }
                    }
                    "Tidy" { Invoke-DependencyCommand "npm" @("prune", "--ignore-scripts") }
                }
            }
            "poetry" {
                switch ($Mode) {
                    "Check" { Invoke-DependencyCommand "poetry" @("show", "--outdated") }
                    "Update" { Invoke-DependencyCommand "poetry" @("update") }
                    "Tidy" { Invoke-DependencyCommand "poetry" @("sync", "--no-root") }
                }
            }
        }
        Write-Host "[OK] $($project.Directory)" -ForegroundColor Green
    }
    catch {
        $FailedProjects += $project.Directory
        Write-Host "[FAILED] $($project.Directory): $_" -ForegroundColor Red
    }
    finally {
        if ($locationPushed) {
            Pop-Location
        }
    }
}

Write-Host "`n=== Dependency Maintenance Summary ===" -ForegroundColor Cyan
if ($FailedProjects.Count -gt 0) {
    Write-Host "[FAILED] $($FailedProjects -join ', ')" -ForegroundColor Red
    exit 1
}
Write-Host "[OK] $Mode completed for all selected projects." -ForegroundColor Green
exit 0
