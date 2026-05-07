import * as aws from "@pulumi/aws";
import * as pulumi from "@pulumi/pulumi";

const cfg = new pulumi.Config();
const lambdaZipPath = cfg.get("lambdaZipPath") ?? "../../dist/highlight-lambda.zip";

const siteBucket = new aws.s3.Bucket("site-bucket", {
  forceDestroy: false
});

new aws.s3.BucketPublicAccessBlock("site-bucket-pab", {
  bucket: siteBucket.id,
  blockPublicAcls: true,
  blockPublicPolicy: true,
  ignorePublicAcls: true,
  restrictPublicBuckets: true
});

const lambdaRole = new aws.iam.Role("highlight-lambda-role", {
  assumeRolePolicy: aws.iam.assumeRolePolicyForPrincipal({ Service: "lambda.amazonaws.com" })
});

new aws.iam.RolePolicyAttachment("highlight-lambda-basic-exec", {
  role: lambdaRole.name,
  policyArn: aws.iam.ManagedPolicies.AWSLambdaBasicExecutionRole
});

const highlightLambda = new aws.lambda.Function("highlight-lambda", {
  role: lambdaRole.arn,
  runtime: "provided.al2023",
  architectures: ["arm64"],
  handler: "bootstrap",
  code: new pulumi.asset.FileArchive(lambdaZipPath),
  environment: {
    variables: {
      GOPLS_BIN: "./gopls"
    }
  },
  timeout: 30,
  memorySize: 512
});

const httpApi = new aws.apigatewayv2.Api("highlight-api", {
  protocolType: "HTTP",
  corsConfiguration: {
    allowMethods: ["POST", "OPTIONS"],
    allowOrigins: ["*"],
    allowHeaders: ["content-type"]
  }
});

const integration = new aws.apigatewayv2.Integration("highlight-integration", {
  apiId: httpApi.id,
  integrationType: "AWS_PROXY",
  integrationUri: highlightLambda.arn,
  payloadFormatVersion: "2.0"
});

new aws.apigatewayv2.Route("highlight-route", {
  apiId: httpApi.id,
  routeKey: "POST /highlight",
  target: pulumi.interpolate`integrations/${integration.id}`
});

const stage = new aws.apigatewayv2.Stage("highlight-stage", {
  apiId: httpApi.id,
  name: "prod",
  autoDeploy: true
});

new aws.lambda.Permission("allow-api-gw", {
  action: "lambda:InvokeFunction",
  function: highlightLambda.name,
  principal: "apigateway.amazonaws.com",
  sourceArn: pulumi.interpolate`${httpApi.executionArn}/*/*`
});

const apiUrl = pulumi.interpolate`${stage.invokeUrl}/highlight`;

const oac = new aws.cloudfront.OriginAccessControl("site-oac", {
  name: "rat-site-oac",
  description: "OAC for rat static site bucket",
  originAccessControlOriginType: "s3",
  signingBehavior: "always",
  signingProtocol: "sigv4"
});

const distribution = new aws.cloudfront.Distribution("site-cdn", {
  enabled: true,
  defaultRootObject: "index.html",
  origins: [
    {
      domainName: siteBucket.bucketRegionalDomainName,
      originId: "site-s3-origin",
      originAccessControlId: oac.id
    }
  ],
  defaultCacheBehavior: {
    targetOriginId: "site-s3-origin",
    viewerProtocolPolicy: "redirect-to-https",
    allowedMethods: ["GET", "HEAD", "OPTIONS"],
    cachedMethods: ["GET", "HEAD"],
    forwardedValues: {
      queryString: false,
      cookies: { forward: "none" }
    }
  },
  restrictions: {
    geoRestriction: { restrictionType: "none" }
  },
  viewerCertificate: {
    cloudfrontDefaultCertificate: true
  },
  customErrorResponses: [
    {
      errorCode: 403,
      responseCode: 200,
      responsePagePath: "/index.html"
    }
  ]
});

new aws.s3.BucketPolicy("site-bucket-policy", {
  bucket: siteBucket.id,
  policy: pulumi.all([siteBucket.arn, distribution.arn]).apply(([bucketArn, distArn]) =>
    JSON.stringify({
      Version: "2012-10-17",
      Statement: [
        {
          Sid: "AllowCloudFrontRead",
          Effect: "Allow",
          Principal: { Service: "cloudfront.amazonaws.com" },
          Action: "s3:GetObject",
          Resource: `${bucketArn}/*`,
          Condition: {
            StringEquals: {
              "AWS:SourceArn": distArn
            }
          }
        }
      ]
    })
  )
});

new aws.s3.BucketObject("site-index", {
  bucket: siteBucket.id,
  key: "index.html",
  contentType: "text/html",
  source: new pulumi.asset.FileAsset("../site/index.html")
});

new aws.s3.BucketObject("site-app", {
  bucket: siteBucket.id,
  key: "app.js",
  contentType: "application/javascript",
  source: new pulumi.asset.FileAsset("../site/app.js")
});

new aws.s3.BucketObject("site-styles", {
  bucket: siteBucket.id,
  key: "styles.css",
  contentType: "text/css",
  source: new pulumi.asset.FileAsset("../site/styles.css")
});

new aws.s3.BucketObject("site-config", {
  bucket: siteBucket.id,
  key: "config.js",
  contentType: "application/javascript",
  content: apiUrl.apply((url) => `window.APP_CONFIG = { apiUrl: ${JSON.stringify(url)} };\n`)
});

export const websiteUrl = pulumi.interpolate`https://${distribution.domainName}`;
export const highlightApiUrl = apiUrl;
