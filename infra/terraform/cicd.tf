data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

data "external" "github_oidc_provider_lookup" {
  count   = var.github_oidc_provider_arn == "" ? 1 : 0
  program = ["sh", "${path.module}/scripts/find-github-oidc-provider.sh"]
}

locals {
  discovered_github_oidc_provider_arn = var.github_oidc_provider_arn != "" ? var.github_oidc_provider_arn : try(data.external.github_oidc_provider_lookup[0].result.arn, "")
  github_oidc_provider_arn = local.discovered_github_oidc_provider_arn != "" ? local.discovered_github_oidc_provider_arn : aws_iam_openid_connect_provider.github_actions[0].arn
  deploy_artifact_bucket_name = var.deploy_artifact_bucket_name != "" ? var.deploy_artifact_bucket_name : "${var.project_name}-${data.aws_caller_identity.current.account_id}-${var.aws_region}-deploy-artifacts"
  github_oidc_subjects = [
    "repo:${var.github_repository}:environment:${var.github_deploy_environment}",
    "repo:${var.github_repository}:ref:refs/heads/${var.github_deploy_branch}",
  ]
}

resource "aws_iam_openid_connect_provider" "github_actions" {
  count = local.discovered_github_oidc_provider_arn == "" ? 1 : 0

  url             = "https://token.actions.githubusercontent.com"
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = var.github_oidc_thumbprints
}

data "aws_iam_policy_document" "github_actions_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [local.github_oidc_provider_arn]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:sub"
      values   = local.github_oidc_subjects
    }
  }
}

resource "aws_iam_role" "github_actions_deploy" {
  name               = "${var.project_name}-github-actions-deploy"
  assume_role_policy = data.aws_iam_policy_document.github_actions_assume_role.json

  tags = {
    Project = var.project_name
  }
}

data "aws_iam_policy_document" "github_actions_deploy" {
  statement {
    sid    = "DeployArtifactBucket"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject",
    ]
    resources = [
      "${aws_s3_bucket.deploy_artifacts.arn}/*",
    ]
  }

  statement {
    sid    = "ListDeployArtifactBucket"
    effect = "Allow"
    actions = [
      "s3:ListBucket",
    ]
    resources = [
      aws_s3_bucket.deploy_artifacts.arn,
    ]
  }

  statement {
    sid    = "SendRunCommand"
    effect = "Allow"
    actions = [
      "ssm:SendCommand",
    ]
    resources = [
      "arn:${data.aws_partition.current.partition}:ssm:${var.aws_region}::document/AWS-RunShellScript",
      "arn:${data.aws_partition.current.partition}:ec2:${var.aws_region}:${data.aws_caller_identity.current.account_id}:instance/${aws_instance.jucobot.id}",
    ]
  }

  statement {
    sid    = "ObserveRunCommand"
    effect = "Allow"
    actions = [
      "ssm:GetCommandInvocation",
      "ssm:ListCommandInvocations",
      "ssm:ListCommands",
      "ec2:DescribeInstances",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "github_actions_deploy" {
  name   = "${var.project_name}-github-actions-deploy"
  role   = aws_iam_role.github_actions_deploy.id
  policy = data.aws_iam_policy_document.github_actions_deploy.json
}

resource "aws_s3_bucket" "deploy_artifacts" {
  bucket = local.deploy_artifact_bucket_name

  tags = {
    Project = var.project_name
  }
}

resource "aws_s3_bucket_public_access_block" "deploy_artifacts" {
  bucket                  = aws_s3_bucket.deploy_artifacts.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_versioning" "deploy_artifacts" {
  bucket = aws_s3_bucket.deploy_artifacts.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "deploy_artifacts" {
  bucket = aws_s3_bucket.deploy_artifacts.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "deploy_artifacts" {
  bucket = aws_s3_bucket.deploy_artifacts.id

  rule {
    id     = "expire-old-release-artifacts"
    status = "Enabled"

    filter {
      prefix = "releases/"
    }

    expiration {
      days = 30
    }

    noncurrent_version_expiration {
      noncurrent_days = 30
    }
  }
}

resource "aws_iam_role" "jucobot_instance" {
  name = "${var.project_name}-instance-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "ec2.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })

  tags = {
    Project = var.project_name
  }
}

resource "aws_iam_role_policy_attachment" "jucobot_instance_ssm_core" {
  role       = aws_iam_role.jucobot_instance.name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

data "aws_iam_policy_document" "jucobot_instance" {
  statement {
    sid    = "ReadDeployArtifacts"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:ListBucket",
    ]
    resources = [
      aws_s3_bucket.deploy_artifacts.arn,
      "${aws_s3_bucket.deploy_artifacts.arn}/*",
    ]
  }

  statement {
    sid    = "ReadProductionParameters"
    effect = "Allow"
    actions = [
      "ssm:GetParameter",
      "ssm:GetParameters",
      "ssm:GetParametersByPath",
    ]
    resources = [
      "arn:${data.aws_partition.current.partition}:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.ssm_prefix}",
      "arn:${data.aws_partition.current.partition}:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.ssm_prefix}/*",
    ]
  }
}

resource "aws_iam_role_policy" "jucobot_instance" {
  name   = "${var.project_name}-instance-access"
  role   = aws_iam_role.jucobot_instance.id
  policy = data.aws_iam_policy_document.jucobot_instance.json
}

resource "aws_iam_instance_profile" "jucobot" {
  name = "${var.project_name}-instance-profile"
  role = aws_iam_role.jucobot_instance.name
}
