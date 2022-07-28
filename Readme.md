# Fact Bench
Fact-Bench is a workload generator to evaluate serverless platfroms such as AWS Lambda or Apache OpenWhisk. FaaS-Bench will support various serverless platfrom APIs to send workload but can also be used for _any_ HTTP API. 

## Introduction
This workload generator is written to be extensible and supports both open- and closed-loop workload generation.

Workloads can be file-driven or using a custom go program. We support the [Fact](https://github.com/faas-facts/fact) Data fromat to record each request. Thus, enabeling easy integration in all platfroms supported by Fact.
