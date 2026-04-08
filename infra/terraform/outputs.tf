output "elastic_ip" {
  description = "Public Elastic IP of the JucoBot EC2 instance"
  value       = aws_eip.jucobot.public_ip
}

output "instance_id" {
  description = "EC2 instance ID"
  value       = aws_instance.jucobot.id
}

output "deploy_target" {
  description = "Ready-to-use DEPLOY_TARGET for make deploy-remote"
  value       = "ubuntu@${aws_eip.jucobot.public_ip}"
}

output "ssh_command" {
  description = "SSH command to connect"
  value       = "ssh ubuntu@${aws_eip.jucobot.public_ip}"
}
