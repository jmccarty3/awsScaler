# awsScaler
Automatically scale AWS resources to meet the needs of pending pods.

The scaler is desigend to automaically provided additional AWS resources to a kuberentes cluster when pods are in a prolonged pending state.
The how and what resources are scaled is fully configurable by users.

## Example Config
```YAML
stratagies:
- nodeSelector:
    usage: worker
    foo: bar
  remediators:
    - autoScalingGroup:
        tags:
          foo: bar
- namespace:
   - alpha
     beta
  remediators:
    - autoScalingGroup:
        tags:
          foo: bar
        names:
          asg-foobar
- remediators:
  - autoScalingGroup:
      selfTags:
      - api-server
```
### Quick Explination:
1. Any Pod with a node selector containing both "usage=worker" and "foo=bar" will cause the scaler to locate autoscaling groups with tags "foo=bar" and scale up the desired amount
2. Any Pod within namespace "alpha" or "beta" will cause the scaler to locate and attempt to scale an autoscaling group tagged "foo=bar" or named "asg-foobar".
3. Any Pod will cause the scaler to attempt to scale up an autoscaling group with the same key/value pair for "api-server" that the scaler is assoicated with.

### Important Notes
* During a remediation cycle, a pod may only match a single stratagy (if the stratagy was able to take action)
* If multiple autoscaling groups are used within a stratagy, each will have an change for scaling to remediate the pending pods
* Autoscaling groups may be ordered using the tag "scaler_priority"
* Groups found by tag are evaluated every time a stratagy is executed
* When possible, the resources (CPU/MEMORY) of pods will be measured against the resources provided by the Instance Type. This allows multiple pods to possible be "remediated" by a single server scaling. Or by scaling multiple servers as needed
