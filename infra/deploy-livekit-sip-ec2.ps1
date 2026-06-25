param(
    [string]$Profile = "default",
    [string]$Region = "eu-west-2",
    [string]$StackName = "livekit-sip-test",
    [string]$ProjectName = "livekit-sip-test",
    [Parameter(Mandatory = $true)]
    [string]$KeyName,
    [Parameter(Mandatory = $true)]
    [string]$AdminCidr,
    [string]$SipIngressCidr = "0.0.0.0/0",
    [string]$RtpIngressCidr = "0.0.0.0/0",
    [string]$InstanceType = "t3.small",
    [ValidateSet("x8664", "arm64")]
    [string]$Architecture = "x8664",
    [int]$SipPort = 5060,
    [int]$RtpPortStart = 20000,
    [int]$RtpPortEnd = 20100
)

$ErrorActionPreference = "Stop"

$TemplatePath = Join-Path $PSScriptRoot "livekit-sip-ec2.yaml"

aws cloudformation deploy `
    --profile $Profile `
    --region $Region `
    --stack-name $StackName `
    --template-file $TemplatePath `
    --capabilities CAPABILITY_IAM `
    --parameter-overrides `
        ProjectName=$ProjectName `
        KeyName=$KeyName `
        AdminCidr=$AdminCidr `
        SipIngressCidr=$SipIngressCidr `
        RtpIngressCidr=$RtpIngressCidr `
        InstanceType=$InstanceType `
        Architecture=$Architecture `
        SipPort=$SipPort `
        RtpPortStart=$RtpPortStart `
        RtpPortEnd=$RtpPortEnd

aws cloudformation describe-stacks `
    --profile $Profile `
    --region $Region `
    --stack-name $StackName `
    --query "Stacks[0].Outputs[*].{Key:OutputKey,Value:OutputValue}" `
    --output table
