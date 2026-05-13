# Run-Tests.ps1
# 
# Consolidated test runner for PubNightPicker
# 
# Usage:
#   ./run-tests.ps1 -Integration                      # Run integration tests (default)
#   ./run-tests.ps1 -Unit                             # Run Python unit tests
#   ./run-tests.ps1 -Node                             # Run Node/React unit tests (vitest)
#   ./run-tests.ps1 -E2E                              # Run E2E tests
#   ./run-tests.ps1 -Tox                              # Run tox (full test suite + linting)
#   ./run-tests.ps1 -Black                            # Run black linting only
#   ./run-tests.ps1 -Typecheck                        # Run React TypeScript typecheck
#   ./run-tests.ps1 -Lint                             # Run React ESLint (rules-of-hooks etc.)
#   ./run-tests.ps1 -All                              # Run all test suites
#   ./run-tests.ps1 -Integration -VerboseOutput      # Integration with verbose output
#   ./run-tests.ps1 -Integration -k test_chat         # Forward pytest args (integration only)
#

param(
    [switch]$Unit,
    [switch]$Integration,
    [switch]$E2E,
    [switch]$Node,
    [switch]$Tox,
    [switch]$Black,
    [switch]$Typecheck,
    [switch]$Lint,
    [switch]$All,
    [switch]$VerboseOutput,
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$PytestArgs
)

$ErrorActionPreference = "Stop"

# Resolve workspace root
$WorkspaceRoot = $PSScriptRoot
$FirebaseSubDir = Join-Path $WorkspaceRoot "firebase_sub"
$E2EDir = Join-Path $WorkspaceRoot "e2e"
$ReactDir = Join-Path $WorkspaceRoot "react"

# Default to Integration if no suite specified
if (-not $Unit -and -not $Integration -and -not $E2E -and -not $Node -and -not $Tox -and -not $Black -and -not $Typecheck -and -not $Lint -and -not $All) {
    $Integration = $true
}

# If All is specified, run all suites
if ($All) {
    $Unit = $true
    $Integration = $true
    $E2E = $true
    $Node = $true
    $Tox = $true
    $Black = $true
    $Typecheck = $true
    $Lint = $true
}

$FailedSuites = @()

# ======================
# Unit Tests
# ======================
if ($Unit) {
    Write-Host "`n=== Running Unit Tests ===" -ForegroundColor Cyan
    
    Push-Location $FirebaseSubDir
    try {
        $cmd = @("poetry", "run", "pytest", "-m", "not integration", "--tb=short")
        if ($VerboseOutput) {
            $cmd += "-v"
        }
        
        Write-Host "[Runner] Command: $($cmd -join ' ')" -ForegroundColor Gray
        & $cmd[0] $cmd[1..($cmd.Length-1)]
        
        if ($LASTEXITCODE -ne 0) {
            $FailedSuites += "Unit Tests"
            Write-Host "[FAILED] Unit tests FAILED" -ForegroundColor Red
        } else {
            Write-Host "[OK] Unit tests PASSED" -ForegroundColor Green
        }
    }
    finally {
        Pop-Location
    }
}

# ======================
# Integration Tests
# ======================
if ($Integration) {
    Write-Host "`n=== Running Integration Tests ===" -ForegroundColor Cyan
    
    Push-Location $FirebaseSubDir
    try {
        $cmd = @("poetry", "run", "python", "..\\run_integration_tests.py")
        
        # Forward any additional pytest args
        if ($PytestArgs.Count -gt 0) {
            $cmd += $PytestArgs
        }
        
        Write-Host "[Runner] Command: $($cmd -join ' ')" -ForegroundColor Gray
        & $cmd[0] $cmd[1..($cmd.Length-1)]
        
        if ($LASTEXITCODE -ne 0) {
            $FailedSuites += "Integration Tests"
            Write-Host "[FAILED] Integration tests FAILED" -ForegroundColor Red
        } else {
            Write-Host "[OK] Integration tests PASSED" -ForegroundColor Green
        }
    }
    finally {
        Pop-Location
    }
}

# ======================
# E2E Tests
# ======================
if ($E2E) {
    Write-Host "`n=== Running E2E Tests ===" -ForegroundColor Cyan
    
    # First ensure Node dependencies are installed
    Write-Host "[Runner] Ensuring Node dependencies..." -ForegroundColor Gray
    Push-Location $E2EDir
    try {
        # Install dependencies if not already installed
        if (-not (Test-Path (Join-Path $E2EDir "node_modules"))) {
            Write-Host "[Runner] Installing npm dependencies..."
            & npm install
            if ($LASTEXITCODE -ne 0) {
                Write-Host "[Runner] npm install failed" -ForegroundColor Red
                $FailedSuites += "E2E Tests (npm install)"
            }
        }
    }
    finally {
        Pop-Location
    }

    # Run E2E test runner
    Push-Location $FirebaseSubDir
    try {
        $cmd = @("poetry", "run", "python", "..\e2e\run_e2e_tests.py")
        
        if ($VerboseOutput) {
            $cmd += "--verbose"
        }
        
        Write-Host "[Runner] Command: $($cmd -join ' ')" -ForegroundColor Gray
        & $cmd[0] $cmd[1..($cmd.Length-1)]
        
        if ($LASTEXITCODE -ne 0) {
            $FailedSuites += "E2E Tests"
            Write-Host "[FAILED] E2E tests FAILED" -ForegroundColor Red
        } else {
            Write-Host "[OK] E2E tests PASSED" -ForegroundColor Green
        }
    }
    finally {
        Pop-Location
    }
}

# ======================
# Node/React Unit Tests
# ======================
if ($Node) {
    Write-Host "`n=== Running Node/React Unit Tests ===" -ForegroundColor Cyan
    
    Push-Location $ReactDir
    try {
        # Ensure dependencies are installed
        if (-not (Test-Path (Join-Path $ReactDir "node_modules"))) {
            Write-Host "[Runner] Installing npm dependencies..."
            & npm install
            if ($LASTEXITCODE -ne 0) {
                Write-Host "[FAILED] npm install failed" -ForegroundColor Red
                $FailedSuites += "Node Unit Tests (npm install)"
            }
        }
        
        $cmd = @("npm", "test", "--", "--run")
        if ($VerboseOutput) {
            $cmd += "--reporter=verbose"
        }
        
        Write-Host "[Runner] Command: $($cmd -join ' ')" -ForegroundColor Gray
        & $cmd[0] $cmd[1..($cmd.Length-1)]
        
        if ($LASTEXITCODE -ne 0) {
            $FailedSuites += "Node Unit Tests"
            Write-Host "[FAILED] Node unit tests FAILED" -ForegroundColor Red
        } else {
            Write-Host "[OK] Node unit tests PASSED" -ForegroundColor Green
        }
    }
    finally {
        Pop-Location
    }
}

# ======================
# React Typecheck
# ======================
if ($Typecheck) {
    Write-Host "`n=== Running React TypeScript Typecheck ===" -ForegroundColor Cyan

    Push-Location $ReactDir
    try {
        & npm run typecheck

        if ($LASTEXITCODE -ne 0) {
            $FailedSuites += "React Typecheck"
            Write-Host "[FAILED] React typecheck FAILED" -ForegroundColor Red
        } else {
            Write-Host "[OK] React typecheck PASSED" -ForegroundColor Green
        }
    }
    finally {
        Pop-Location
    }
}

# ======================
# React ESLint (rules-of-hooks)
# ======================
if ($Lint) {
    Write-Host "`n=== Running React ESLint ==" -ForegroundColor Cyan

    Push-Location $ReactDir
    try {
        & npm run lint

        if ($LASTEXITCODE -ne 0) {
            $FailedSuites += "React Lint"
            Write-Host "[FAILED] React lint FAILED" -ForegroundColor Red
        } else {
            Write-Host "[OK] React lint PASSED" -ForegroundColor Green
        }
    }
    finally {
        Pop-Location
    }
}

# ======================
# Tox Tests (py312 + integration)
# ======================
if ($Tox) {
    Write-Host "`n=== Running Tox (py312 environment) ===" -ForegroundColor Cyan
    
    Push-Location $FirebaseSubDir
    try {
        $cmd = @("poetry", "run", "tox", "-e", "py312")
        if ($VerboseOutput) {
            $cmd += "-v"
        }
        
        Write-Host "[Runner] Command: $($cmd -join ' ')" -ForegroundColor Gray
        & $cmd[0] $cmd[1..($cmd.Length-1)]
        
        if ($LASTEXITCODE -ne 0) {
            $FailedSuites += "Tox (py312)"
            Write-Host "[FAILED] Tox tests FAILED" -ForegroundColor Red
        } else {
            Write-Host "[OK] Tox tests PASSED" -ForegroundColor Green
        }
    }
    finally {
        Pop-Location
    }
}

# ======================
# Black Linting
# ======================
if ($Black) {
    Write-Host "`n=== Running Black Linting ===" -ForegroundColor Cyan
    
    Push-Location $FirebaseSubDir
    try {
        $cmd = @("poetry", "run", "tox", "-e", "black")
        
        Write-Host "[Runner] Command: $($cmd -join ' ')" -ForegroundColor Gray
        & $cmd[0] $cmd[1..($cmd.Length-1)]
        
        if ($LASTEXITCODE -ne 0) {
            $FailedSuites += "Black Linting"
            Write-Host "[FAILED] Black linting FAILED" -ForegroundColor Red
        } else {
            Write-Host "[OK] Black linting PASSED" -ForegroundColor Green
        }
    }
    finally {
        Pop-Location
    }
}

# ======================
# Summary
# ======================
Write-Host "`n=== Test Summary ===" -ForegroundColor Cyan

if ($FailedSuites.Count -eq 0) {
    Write-Host "[OK] All test suites PASSED" -ForegroundColor Green
    exit 0
} else {
    Write-Host "[FAILED] Failed suites:" -ForegroundColor Red
    foreach ($suite in $FailedSuites) {
        Write-Host "  - $suite" -ForegroundColor Red
    }
    exit 1
}
