# SSHDock

SSHDock is an SSH-native platform for deploying and operating Compose applications on one node.

## Public examples

**Framework quickstart**:
A runnable public example built from a framework's smallest official starter, with the starter application kept intact and SSHDock-specific integration added around it.
_Avoid_: Framework demo, tutorial application, custom showcase

**Minimal starter**:
The smallest official framework starter that exposes enough real behavior to prove a production deployment without adding application features.
_Avoid_: Handwritten sample, showcase template

**Starter integrity**:
The selected starter's application files and dependency manifest remain unchanged from their official source. Reproducibility comes from the upstream lockfile and recorded source revision or generator version.
_Avoid_: SSHDock-flavored starter, rewritten starter

**Minimal image variant**:
The smallest officially published and upstream-supported container image variant within the example's simplest viable topology. Alpine is preferred when it does not require extra services or agent-authored configuration; otherwise the smallest supported variant is used without rebuilding the application onto a different base.
_Avoid_: Custom minimal image, unofficial image

**Feature lab**:
A minimal executable overlay that reuses a canonical public example and adds only the fixtures needed to reproduce and verify one SSHDock workflow.
_Avoid_: Documentation-only walkthrough, duplicate example application
