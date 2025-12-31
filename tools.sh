#!/bin/bash

# =============================================================================
# TOOLS INSTALLER - Phiên bản cải tiến
# =============================================================================
# **Purpose** (Mục đích): Cài đặt bộ công cụ DevOps/IaC (Azure CLI, Terraform, Ansible, PowerShell)
# **Author** (Tác giả): TerraForm Project
# **Version** (Phiên bản): 2.0 - Với **idempotency** (bất biến) và **error handling** (xử lý lỗi)
# =============================================================================

# **Strict error handling** (Xử lý lỗi nghiêm ngặt)
set -euo pipefail

# =============================================================================
# CẤU HÌNH & HẰNG SỐ
# =============================================================================
readonly SCRIPT_NAME="$(basename "$0")"
readonly LOG_FILE="/var/log/tools-installer.log"
readonly HASHICORP_GPG_KEY="/usr/share/keyrings/hashicorp-archive-keyring.gpg"
readonly HASHICORP_REPO="/etc/apt/sources.list.d/hashicorp.list"
readonly MICROSOFT_GPG_KEY="/usr/share/keyrings/microsoft-prod.gpg"

# **Colors** (Màu sắc) cho output
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly NC='\033[0m' # No Color

# =============================================================================
# HÀM TIỆN ÍCH - LOGGING
# =============================================================================
log_info() {
    local message="$1"
    echo -e "${BLUE}ℹ️  [INFO]${NC} $message"
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] [INFO] $message" >> "$LOG_FILE" 2>/dev/null || true
}

log_success() {
    local message="$1"
    echo -e "${GREEN}✅ [SUCCESS]${NC} $message"
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] [SUCCESS] $message" >> "$LOG_FILE" 2>/dev/null || true
}

log_warning() {
    local message="$1"
    echo -e "${YELLOW}⚠️  [WARNING]${NC} $message"
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] [WARNING] $message" >> "$LOG_FILE" 2>/dev/null || true
}

log_error() {
    local message="$1"
    echo -e "${RED}❌ [ERROR]${NC} $message" >&2
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] [ERROR] $message" >> "$LOG_FILE" 2>/dev/null || true
}

# =============================================================================
# HÀM KIỂM TRA
# =============================================================================
check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "Script này cần chạy với quyền **root** (sudo)."
        exit 1
    fi
}

check_os() {
    if ! command -v apt &>/dev/null; then
        log_error "Script chỉ hỗ trợ hệ điều hành **Debian/Ubuntu**."
        exit 1
    fi
    log_info "Hệ điều hành: $(lsb_release -ds 2>/dev/null || echo 'Debian-based')"
}

is_installed() {
    local cmd="$1"
    command -v "$cmd" &>/dev/null
}

# =============================================================================
# CÀI ĐẶT DEPENDENCIES CƠ BẢN
# =============================================================================
install_base_dependencies() {
    log_info "Đang cài đặt **base dependencies** (phụ thuộc cơ bản)..."
    
    local packages=(
        "curl"
        "wget"
        "gnupg"
        "software-properties-common"
        "apt-transport-https"
        "ca-certificates"
        "python3-pip"
        "sshpass"
        "lsb-release"
    )
    
    apt-get update -qq
    
    for pkg in "${packages[@]}"; do
        if dpkg -l "$pkg" &>/dev/null; then
            log_info "  - $pkg: đã có sẵn"
        else
            log_info "  - Đang cài đặt $pkg..."
            apt-get install -y -qq "$pkg"
        fi
    done
    
    log_success "Base dependencies đã sẵn sàng"
}

# =============================================================================
# CÀI ĐẶT AZURE CLI
# =============================================================================
install_azure_cli() {
    log_info "Kiểm tra **Azure CLI**..."
    
    if is_installed az; then
        local current_version
        current_version=$(az version --query '"azure-cli"' -o tsv 2>/dev/null || echo "unknown")
        log_success "Azure CLI đã có (version: $current_version)"
        return 0
    fi
    
    log_info "Đang cài đặt **Azure CLI**..."
    
    # Phương pháp chính thức từ Microsoft
    curl -sL https://aka.ms/InstallAzureCLIDeb | bash
    
    if is_installed az; then
        log_success "Azure CLI đã cài đặt thành công"
    else
        log_error "Cài đặt Azure CLI thất bại"
        return 1
    fi
}

# =============================================================================
# CÀI ĐẶT TERRAFORM
# =============================================================================
install_terraform() {
    log_info "Kiểm tra **Terraform**..."
    
    if is_installed terraform; then
        local current_version
        current_version=$(terraform version -json 2>/dev/null | grep -oP '"terraform_version":\s*"\K[^"]+' || terraform --version | head -1)
        log_success "Terraform đã có ($current_version)"
        return 0
    fi
    
    log_info "Đang cài đặt **Terraform**..."
    
    # Thêm GPG key nếu chưa có
    if [[ ! -f "$HASHICORP_GPG_KEY" ]]; then
        log_info "  - Thêm HashiCorp GPG key..."
        wget -qO- https://apt.releases.hashicorp.com/gpg | gpg --dearmor -o "$HASHICORP_GPG_KEY"
    fi
    
    # Thêm repository nếu chưa có
    if [[ ! -f "$HASHICORP_REPO" ]]; then
        log_info "  - Thêm HashiCorp repository..."
        echo "deb [arch=$(dpkg --print-architecture) signed-by=$HASHICORP_GPG_KEY] https://apt.releases.hashicorp.com $(lsb_release -cs) main" > "$HASHICORP_REPO"
    fi
    
    apt-get update -qq
    apt-get install -y -qq terraform
    
    if is_installed terraform; then
        log_success "Terraform đã cài đặt thành công"
    else
        log_error "Cài đặt Terraform thất bại"
        return 1
    fi
}

# =============================================================================
# CÀI ĐẶT ANSIBLE
# =============================================================================
install_ansible() {
    log_info "Kiểm tra **Ansible**..."
    
    if is_installed ansible; then
        local current_version
        current_version=$(ansible --version | head -1 | awk '{print $NF}' | tr -d '[]')
        log_success "Ansible đã có (version: $current_version)"
        return 0
    fi
    
    log_info "Đang cài đặt **Ansible**..."
    
    # Thêm PPA chính thức
    add-apt-repository --yes --update ppa:ansible/ansible
    apt-get install -y -qq ansible
    
    if is_installed ansible; then
        log_success "Ansible đã cài đặt thành công"
    else
        log_error "Cài đặt Ansible thất bại"
        return 1
    fi
}

# =============================================================================
# CÀI ĐẶT NODE.JS
# =============================================================================
install_nodejs() {
    log_info "Kiểm tra **Node.js**..."
    
    if is_installed node; then
        local current_version
        current_version=$(node --version 2>/dev/null || echo "unknown")
        log_success "Node.js đã có (version: $current_version)"
        return 0
    fi
    
    log_info "Đang cài đặt **Node.js 20.x**..."
    
    # Cài đặt từ NodeSource repository
    curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
    apt-get install -y -qq nodejs
    
    if is_installed node; then
        log_success "Node.js đã cài đặt thành công ($(node --version))"
    else
        log_error "Cài đặt Node.js thất bại"
        return 1
    fi
}

# =============================================================================
# CÀI ĐẶT POWERSHELL
# =============================================================================
install_powershell() {
    log_info "Kiểm tra **PowerShell**..."
    
    if is_installed pwsh; then
        local current_version
        current_version=$(pwsh -Command '$PSVersionTable.PSVersion.ToString()' 2>/dev/null || echo "unknown")
        log_success "PowerShell đã có (version: $current_version)"
        return 0
    fi
    
    log_info "Đang cài đặt **PowerShell**..."
    
    local ubuntu_version
    ubuntu_version=$(lsb_release -rs)
    
    # Tải và import Microsoft GPG key
    if [[ ! -f "$MICROSOFT_GPG_KEY" ]]; then
        log_info "  - Thêm Microsoft GPG key..."
        curl -sL "https://packages.microsoft.com/keys/microsoft.asc" | gpg --dearmor -o "$MICROSOFT_GPG_KEY"
    fi
    
    # Thêm Microsoft repository
    local repo_file="/etc/apt/sources.list.d/microsoft-prod.list"
    if [[ ! -f "$repo_file" ]]; then
        log_info "  - Thêm Microsoft repository..."
        echo "deb [arch=$(dpkg --print-architecture) signed-by=$MICROSOFT_GPG_KEY] https://packages.microsoft.com/ubuntu/${ubuntu_version}/prod $(lsb_release -cs) main" > "$repo_file"
    fi
    
    apt-get update -qq
    apt-get install -y -qq powershell
    
    if is_installed pwsh; then
        log_success "PowerShell đã cài đặt thành công"
    else
        log_warning "Cài đặt PowerShell thất bại - có thể không hỗ trợ Ubuntu $ubuntu_version"
        return 1
    fi
}

# =============================================================================
# VERIFICATION - KIỂM TRA KẾT QUẢ
# =============================================================================
verify_installations() {
    echo ""
    echo "=============================================="
    echo "        📋 TỔNG KẾT CÀI ĐẶT"
    echo "=============================================="
    echo ""
    
    local tools=("az:Azure CLI" "terraform:Terraform" "ansible:Ansible" "pwsh:PowerShell" "node:Node.js")
    local all_ok=true
    
    for tool_entry in "${tools[@]}"; do
        local cmd="${tool_entry%%:*}"
        local name="${tool_entry##*:}"
        
        if is_installed "$cmd"; then
            echo -e "${GREEN}✅${NC} $name: $(command -v "$cmd")"
        else
            echo -e "${RED}❌${NC} $name: KHÔNG CÓ"
            all_ok=false
        fi
    done
    
    echo ""
    echo "=============================================="
    
    if $all_ok; then
        log_success "Tất cả công cụ đã được cài đặt thành công!"
    else
        log_warning "Một số công cụ chưa được cài đặt"
    fi
}

# =============================================================================
# CLEANUP - DỌN DẸP
# =============================================================================
cleanup() {
    log_info "Đang dọn dẹp **apt cache**..."
    apt-get clean -qq
    apt-get autoremove -y -qq
    log_success "Đã dọn dẹp cache"
}

# =============================================================================
# HELP - HƯỚNG DẪN SỬ DỤNG
# =============================================================================
show_help() {
    cat << EOF
Usage: sudo $SCRIPT_NAME [OPTIONS]

Cài đặt bộ công cụ DevOps: Azure CLI, Terraform, Ansible, PowerShell

OPTIONS:
    -h, --help          Hiển thị hướng dẫn này
    -a, --all           Cài đặt tất cả công cụ (mặc định)
    --azure-cli         Chỉ cài đặt Azure CLI
    --terraform         Chỉ cài đặt Terraform
    --ansible           Chỉ cài đặt Ansible
    --powershell        Chỉ cài đặt PowerShell
    --nodejs            Chỉ cài đặt Node.js
    --skip-cleanup      Bỏ qua bước dọn dẹp cache
    --verify            Chỉ kiểm tra các công cụ đã cài

EXAMPLES:
    sudo $SCRIPT_NAME                    # Cài tất cả
    sudo $SCRIPT_NAME --terraform        # Chỉ cài Terraform
    sudo $SCRIPT_NAME --verify           # Kiểm tra cài đặt

EOF
}

# =============================================================================
# MAIN - HÀM CHÍNH
# =============================================================================
main() {
    local install_all=true
    local install_azure=false
    local install_tf=false
    local install_ans=false
    local install_ps=false
    local install_node=false
    local skip_cleanup=false
    local verify_only=false
    
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -h|--help)
                show_help
                exit 0
                ;;
            -a|--all)
                install_all=true
                shift
                ;;
            --azure-cli)
                install_all=false
                install_azure=true
                shift
                ;;
            --terraform)
                install_all=false
                install_tf=true
                shift
                ;;
            --ansible)
                install_all=false
                install_ans=true
                shift
                ;;
            --powershell)
                install_all=false
                install_ps=true
                shift
                ;;
            --nodejs)
                install_all=false
                install_node=true
                shift
                ;;
            --skip-cleanup)
                skip_cleanup=true
                shift
                ;;
            --verify)
                verify_only=true
                shift
                ;;
            *)
                log_error "Option không hợp lệ: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    echo ""
    echo "=============================================="
    echo "   🛠️  TOOLS INSTALLER v2.0"
    echo "=============================================="
    echo ""
    
    # Chỉ verify
    if $verify_only; then
        verify_installations
        exit 0
    fi
    
    # Kiểm tra quyền root
    check_root
    check_os
    
    # Tạo log file
    touch "$LOG_FILE" 2>/dev/null || true
    log_info "Bắt đầu cài đặt - Log: $LOG_FILE"
    
    # Cài đặt base dependencies
    install_base_dependencies
    
    # Cài đặt theo yêu cầu
    if $install_all; then
        install_azure_cli
        install_terraform
        install_ansible
        install_powershell
        install_nodejs
    else
        $install_azure && install_azure_cli
        $install_tf && install_terraform
        $install_ans && install_ansible
        $install_ps && install_powershell
        $install_node && install_nodejs
    fi
    
    # Cleanup
    if ! $skip_cleanup; then
        cleanup
    fi
    
    # Verification
    verify_installations
    
    log_info "Hoàn tất! Xem log chi tiết tại: $LOG_FILE"
}

# Chạy main với tất cả arguments
main "$@"
