# Latest Ubuntu 22.04 LTS AMI (Canonical official)
data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"] # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

# SSH key pair
resource "aws_key_pair" "jucobot" {
  key_name   = "${var.project_name}-key"
  public_key = file(pathexpand(var.ssh_public_key_path))

  tags = {
    Project = var.project_name
  }
}

# EC2 instance
resource "aws_instance" "jucobot" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.instance_type
  key_name               = aws_key_pair.jucobot.key_name
  vpc_security_group_ids = [aws_security_group.jucobot.id]
  subnet_id              = tolist(data.aws_subnets.default.ids)[0]

  root_block_device {
    volume_size = var.volume_size
    volume_type = "gp3"
  }

  user_data = templatefile("${path.module}/user-data.sh", {
    tailscale_auth_key = var.tailscale_auth_key
    project_name       = var.project_name
  })

  tags = {
    Name    = "${var.project_name}-server"
    Project = var.project_name
  }

  lifecycle {
    create_before_destroy = true
  }
}
