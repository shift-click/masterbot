# Use default VPC — no custom VPC needed for single-instance deployment.
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
  filter {
    name   = "default-for-az"
    values = ["true"]
  }
}

# --- Security Group ---

resource "aws_security_group" "jucobot" {
  name        = "${var.project_name}-sg"
  description = "JucoBot EC2 security group"
  vpc_id      = data.aws_vpc.default.id

  tags = {
    Name    = "${var.project_name}-sg"
    Project = var.project_name
  }
}

# SSH — admin IP only
resource "aws_vpc_security_group_ingress_rule" "ssh" {
  security_group_id = aws_security_group.jucobot.id
  description       = "SSH from admin"
  ip_protocol       = "tcp"
  from_port         = 22
  to_port           = 22
  cidr_ipv4         = var.admin_cidr_blocks[0]

  tags = { Name = "ssh" }
}

# Additional admin CIDRs for SSH (if more than one)
resource "aws_vpc_security_group_ingress_rule" "ssh_extra" {
  for_each = toset(slice(var.admin_cidr_blocks, 1, length(var.admin_cidr_blocks)))

  security_group_id = aws_security_group.jucobot.id
  description       = "SSH from admin (extra)"
  ip_protocol       = "tcp"
  from_port         = 22
  to_port           = 22
  cidr_ipv4         = each.value

  tags = { Name = "ssh-extra" }
}

# Admin dashboard (optional)
resource "aws_vpc_security_group_ingress_rule" "admin_dashboard" {
  count = var.enable_admin_dashboard ? 1 : 0

  security_group_id = aws_security_group.jucobot.id
  description       = "Admin dashboard"
  ip_protocol       = "tcp"
  from_port         = 8080
  to_port           = 8080
  cidr_ipv4         = "0.0.0.0/0"

  tags = { Name = "admin-dashboard" }
}

# Tailscale UDP (WireGuard transport)
resource "aws_vpc_security_group_ingress_rule" "tailscale" {
  security_group_id = aws_security_group.jucobot.id
  description       = "Tailscale WireGuard"
  ip_protocol       = "udp"
  from_port         = 41641
  to_port           = 41641
  cidr_ipv4         = "0.0.0.0/0"

  tags = { Name = "tailscale" }
}

# Egress — allow all
resource "aws_vpc_security_group_egress_rule" "all" {
  security_group_id = aws_security_group.jucobot.id
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"

  tags = { Name = "all-egress" }
}

# --- Elastic IP ---

resource "aws_eip" "jucobot" {
  domain = "vpc"

  tags = {
    Name    = "${var.project_name}-eip"
    Project = var.project_name
  }
}

resource "aws_eip_association" "jucobot" {
  instance_id   = aws_instance.jucobot.id
  allocation_id = aws_eip.jucobot.id
}
