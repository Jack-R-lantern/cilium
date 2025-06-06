.. only:: not (epub or latex or html)

    WARNING: You are looking at unreleased Cilium documentation.
    Please use the official rendered version released here:
    https://docs.cilium.io

.. _testsuite-legacy:

End-To-End Testing Framework (Legacy)
=====================================

.. warning::
   The Ginkgo end-to-end testing framework is deprecated. New end-to-end
   tests should be implemented using the `cilium-cli
   <https://github.com/cilium/cilium-cli/#connectivity-check>`_ connectivity
   testing framework. For more information, see :ref:`testsuite`.

Introduction
~~~~~~~~~~~~

This section provides an overview of the two modes available for running
Cilium's end-to-end tests locally: Kubeconfig and similar to GitHub Actions (GHA).
It offers instructions on setting up and running tests in these modes.

Before proceeding, it is recommended to familiarize yourself with Ginkgo by
reading the `Ginkgo Getting-Started Guide
<https://onsi.github.io/ginkgo/#getting-started>`_. You
can also run the `example tests
<https://github.com/onsi/composition-ginkgo-example>`_ to get a feel for the
Ginkgo workflow.

The tests in the ``test`` directory are built on top of Ginkgo and utilize the
Ginkgo ``focus`` concept to determine which Kubernetes nodes are necessary to
run specific tests. All test names must begin with one of the following
prefixes:

- ``Runtime``: Tests Cilium in a runtime environment running on a single node.
- ``K8s``: Sets up a small multi-node Kubernetes environment for testing features
  beyond a single host and Kubernetes-specific functionalities.


Running Tests with GitHub Actions (GHA)
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

GitHub Actions provide an alternative mode for running Cilium's end-to-end tests.
The configuration is set up to closely match the environment used in GHA. Refer
to the relevant documentation for instructions on running tests using GHA.

Running End-To-End Tests
~~~~~~~~~~~~~~~~~~~~~~~~

Running Locally Ginkgo Tests based on Ginkgo's GitHub Workflow
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

Although it is not possible to run ``conformance-ginkgo.yaml`` or
``conformance-runtime.yaml`` locally, it is possible to setup an environment
similar to the one used on GitHub.

The following example will provide the steps to run one of the tests of the
focus ``f09-datapath-misc-2`` on Kubernetes ``1.27`` with the kernel ``net-next``
for the commit SHA ``7b368923823e63c9824ea2b5ee4dc026bc4d5cd8``.

You can also perform these steps automatically using the script
``contrib/scripts/run-gh-ginkgo-workflow.sh``. Run this script with ``-h`` for
usage information.

#. Download dependencies locally (``helm``, ``ginkgo``).

   For ``helm``, the instructions can be found `here <https://helm.sh/docs/intro/install/>`_

   .. code-block:: shell-session

      $ HELM_VERSION=v3.13.1
      $ wget "https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz"
      $ tar -xf "helm-v${HELM_VERSION}-linux-amd64.tar.gz"
      $ mv linux-amd64/helm ./helm

   Store these dependencies under a specific directory that will be used to run
   Qemu in the next steps.

   For ``ginkgo``, we will be using the same version used on GitHub action:

   .. code-block:: shell-session

      $ cd ~/
      $ go install github.com/onsi/ginkgo/ginkgo@v1.16.5
      $ ${GOPATH}/bin/ginkgo version
      Ginkgo Version 1.16.5

#. Build the Ginkgo tests locally. This will create a binary named ``test.test``
   which we can use later on to run our tests.

   .. code-block:: shell-session

      $ cd github.com/cilium/cilium/test
      $ ${GOPATH}/bin/ginkgo build

#. Provision VMs using Qemu:

   * Retrieve the image tag for the k8s and kernel versions that will be used for
     testing by checking the file ``.github/actions/ginkgo/main-k8s-versions.yaml``.

     For example:

        - kernel: ``bpf-next-20230526.105339@sha256:4133d4e09b1e86ac175df8d899873180281bb4220dc43e2566c47b0241637411``
        - k8s: ``kindest/node:v1.27.1@sha256:b7d12ed662b873bd8510879c1846e87c7e676a79fefc93e17b2a52989d3ff42b``

   * Store the compressed VM image under a directory (``/tmp/_images``).

   .. code-block:: shell-session

      $ mkdir -p /tmp/_images
      $ kernel_tag="bpf-next-20230526.105339@sha256:4133d4e09b1e86ac175df8d899873180281bb4220dc43e2566c47b0241637411"
      $ docker run -v /tmp/_images:/mnt/images \
         "quay.io/lvh-images/kind:${kernel_tag}" \
         cp -r /data/images/. /mnt/images/

   * Uncompress the VM image into a directory.

   .. code-block:: shell-session

      $ zstd -d /tmp/_images/kind_*.qcow2.zst -o /tmp/_images/datapath-conformance.qcow2

   * Provision the VM. **Qemu will use the current terminal to provision the VM
     and will mount the current directory into the VM under** ``/host``.

   .. code-block:: shell-session

      $ qemu-system-x86_64 \
          -nodefaults \
          -no-reboot \
          -smp 4 \
          -m 12G \
          -enable-kvm \
          -cpu host \
          -drive file=/tmp/_images/datapath-conformance.qcow2,if=virtio,index=0,media=disk \
          -netdev user,id=user.0,hostfwd=tcp::2222-:22 \
          -device virtio-net-pci,netdev=user.0 \
          -fsdev local,id=host_id,path=./,security_model=none \
          -device virtio-9p-pci,fsdev=host_id,mount_tag=host_mount \
          -serial mon:stdio

#. Installing dependencies in the VM (``helm``).

   .. code-block:: shell-session

      $ ssh -p 2222 -o "StrictHostKeyChecking=no" root@localhost
      # echo "nameserver 8.8.8.8" > /etc/resolv.conf
      # git config --global --add safe.directory /host
      # cp /host/helm /usr/bin

   .. _install_kind:

#. The VM is ready to be used for tests. Similarly to the GitHub Action, Kind
   will also be used to run the CI. The provisioning of Kind is different
   depending on the kernel version that is used, i.e., ginkgo tests are meant
   to run on differently when running on bpf-next.

   .. code-block:: shell-session

      $ ssh -p 2222 -o "StrictHostKeyChecking=no" root@localhost
      # cd /host/
      # kernel_tag="bpf-next-20230526.105339@sha256:4133d4e09b1e86ac175df8d899873180281bb4220dc43e2566c47b0241637411"
      # kubernetes_image="kindest/node:v1.27.1@sha256:b7d12ed662b873bd8510879c1846e87c7e676a79fefc93e17b2a52989d3ff42b"
      # ip_family="dual" # replace with "ipv4" if k8s 1.19
      #
      # if [[ "${kernel_tag}" == bpf-next-* ]]; then
      #  ./contrib/scripts/kind.sh "" 2 "" "${kubernetes_image}" "none" "${ip_family}"
      #  kubectl label node kind-worker2 cilium.io/ci-node=kind-worker2
      #  # Avoid re-labeling this node by setting "node-role.kubernetes.io/controlplane"
      #  kubectl label node kind-worker2 node-role.kubernetes.io/controlplane=
      # else
      #   ./contrib/scripts/kind.sh "" 1 "" "${kubernetes_image}" "iptables" "${ip_family}"
      # fi
      # git config --global --add safe.directory /cilium

   Verify that kind is running inside the VM:

   .. code-block:: shell-session

      $ ssh -p 2222 -o "StrictHostKeyChecking=no" root@localhost
      # kubectl get pods -A
      NAMESPACE            NAME                                         READY   STATUS    RESTARTS   AGE
      kube-system          coredns-787d4945fb-hqzpb                     0/1     Pending   0          42s
      kube-system          coredns-787d4945fb-tkq86                     0/1     Pending   0          42s
      kube-system          etcd-kind-control-plane                      1/1     Running   0          57s
      kube-system          kube-apiserver-kind-control-plane            1/1     Running   0          57s
      kube-system          kube-controller-manager-kind-control-plane   1/1     Running   0          56s
      kube-system          kube-scheduler-kind-control-plane            1/1     Running   0          56s
      local-path-storage   local-path-provisioner-6bd6454576-648bk      0/1     Pending   0          42s

#. Now that Kind is provisioned, the tests can be executed inside the VM.
   Let us first retrieve the focus regex, under ``cliFocus``, of
   ``f09-datapath-misc-2`` from ``.github/actions/ginkgo/main-focus.yaml``.

   * ``cliFocus="K8sDatapathConfig Check|K8sDatapathConfig IPv4Only|K8sDatapathConfig High-scale|K8sDatapathConfig Iptables|K8sDatapathConfig IPv4Only|K8sDatapathConfig IPv6|K8sDatapathConfig Transparent"``

   Run the binary ``test.test`` that was compiled in the previous step. The
   following code block is exactly the same as used on the GitHub workflow with
   one exception: the flag ``-cilium.holdEnvironment=true``. This flag
   will hold the testing environment in case the test fails to allow for further
   diagnosis of the current cluster.

   .. code-block:: shell-session

      $ ssh -p 2222 -o "StrictHostKeyChecking=no" root@localhost
      # cd /host/test
      # kernel_tag="bpf-next-20230526.105339@sha256:4133d4e09b1e86ac175df8d899873180281bb4220dc43e2566c47b0241637411"
      # k8s_version="1.27"
      #
      # export K8S_NODES=2
      # export NETNEXT=0
      # export K8S_VERSION="${k8s_version}"
      # export CNI_INTEGRATION=kind
      # export INTEGRATION_TESTS=true
      #
      # if [[ "${kernel_tag}" == bpf-next-* ]]; then
      #    export KERNEL=net-next
      #    export NETNEXT=1
      #    export KUBEPROXY=0
      #    export K8S_NODES=3
      #    export NO_CILIUM_ON_NODES=kind-worker2
      # elif [[ "${kernel_tag}" == 5.4-* ]]; then
      #    export KERNEL=54
      # fi
      #
      # # GitHub actions do not support IPv6 connectivity to outside
      # # world. If the infrastructure environment supports it, then
      # # this line can be removed
      # export CILIUM_NO_IPV6_OUTSIDE=true
      #
      # commit_sha="7b368923823e63c9824ea2b5ee4dc026bc4d5cd8"
      # cliFocus="K8sDatapathConfig Check|K8sDatapathConfig IPv4Only|K8sDatapathConfig High-scale|K8sDatapathConfig Iptables|K8sDatapathConfig IPv4Only|K8sDatapathConfig IPv6|K8sDatapathConfig Transparent"
      # quay_org="cilium"
      #
      # ./test.test \
        --ginkgo.focus="${cliFocus}" \
        --ginkgo.skip="" \
        --ginkgo.seed=1679952881 \
        --ginkgo.v -- \
        -cilium.image=quay.io/${quay_org}/cilium-ci \
        -cilium.tag=${commit_sha}  \
        -cilium.operator-image=quay.io/${quay_org}/operator \
        -cilium.operator-tag=${commit_sha} \
        -cilium.hubble-relay-image=quay.io/${quay_org}/hubble-relay-ci \
        -cilium.hubble-relay-tag=${commit_sha} \
        -cilium.kubeconfig=/root/.kube/config \
        -cilium.operator-suffix=-ci \
        -cilium.holdEnvironment=true
      Using CNI_INTEGRATION="kind"
      Running Suite: Suite-k8s-1.27
      =============================
      Random Seed: 1679952881
      Will run 7 of 132 specs

#. Wait until the test execution completes.

   .. code-block:: shell-session

      Ran 7 of 132 Specs in 721.007 seconds
      SUCCESS! -- 7 Passed | 0 Failed | 0 Pending | 125 Skipped

#. Clean up.

   Once tests are performed, terminate qemu to halt the VM:

   .. code-block:: shell-session

      $ pkill qemu-system-x86

   The VM state is kept in ``/tmp/_images/datapath-conformance.qcow2`` and the
   dependencies are installed. Thus steps up to and excluding step
   :ref:`installing kind <install_kind>` can be skipped next time and the VM
   state can be re-used from step :ref:`installing kind <install_kind>` onwards.

Running Runtime Tests
^^^^^^^^^^^^^^^^^^^^^

To run all of the runtime tests, execute the following command from the ``test`` directory:

.. code-block:: shell-session

    INTEGRATION_TESTS=true ginkgo --focus="Runtime"

Ginkgo searches for all tests in all subdirectories that are "named" beginning
with the string "Runtime" and contain any characters after it. For instance,
here is an example showing what tests will be ran using Ginkgo's dryRun option:

.. code-block:: shell-session

    $ INTEGRATION_TESTS=true ginkgo --focus="Runtime" -dryRun
    Running Suite: runtime
    ======================
    Random Seed: 1516125117
    Will run 42 of 164 specs
    ................
    RuntimePolicyEnforcement Policy Enforcement Always
      Always to Never with policy
      /Users/ianvernon/go/src/github.com/cilium/cilium/test/runtime/Policies.go:258
    •
    ------------------------------
    RuntimePolicyEnforcement Policy Enforcement Always
      Always to Never without policy
      /Users/ianvernon/go/src/github.com/cilium/cilium/test/runtime/Policies.go:293
    •
    ------------------------------
    RuntimePolicyEnforcement Policy Enforcement Never
      Container creation
      /Users/ianvernon/go/src/github.com/cilium/cilium/test/runtime/Policies.go:332
    •
    ------------------------------
    RuntimePolicyEnforcement Policy Enforcement Never
      Never to default with policy
      /Users/ianvernon/go/src/github.com/cilium/cilium/test/runtime/Policies.go:349
    .................
    Ran 42 of 164 Specs in 0.002 seconds
    SUCCESS! -- 0 Passed | 0 Failed | 0 Pending | 122 Skipped PASS

    Ginkgo ran 1 suite in 1.830262168s
    Test Suite Passed

The output has been truncated. For more information about this functionality,
consult the aforementioned Ginkgo documentation.

Available CLI Options
^^^^^^^^^^^^^^^^^^^^^

For more advanced workflows, check the list of available custom options for the Cilium
framework in the ``test/`` directory and interact with ginkgo directly:

.. code-block:: shell-session

    $ cd test/
    $ ginkgo . -- -cilium.help
      -cilium.SSHConfig string
            Specify a custom command to fetch SSH configuration (eg: 'vagrant ssh-config')
      -cilium.help
            Display this help message.
      -cilium.holdEnvironment
            On failure, hold the environment in its current state
      -cilium.hubble-relay-image string
            Specifies which image of hubble-relay to use during tests
      -cilium.hubble-relay-tag string
            Specifies which tag of hubble-relay to use during tests
      -cilium.image string
            Specifies which image of cilium to use during tests
      -cilium.kubeconfig string
            Kubeconfig to be used for k8s tests
      -cilium.multinode
            Enable tests across multiple nodes. If disabled, such tests may silently pass (default true)
      -cilium.operator-image string
            Specifies which image of cilium-operator to use during tests
      -cilium.operator-tag string
            Specifies which tag of cilium-operator to use during tests
      -cilium.passCLIEnvironment
            Pass the environment invoking ginkgo, including PATH, to subcommands
      -cilium.showCommands
            Output which commands are ran to stdout
      -cilium.skipLogs
            skip gathering logs if a test fails
      -cilium.tag string
            Specifies which tag of cilium to use during tests
      -cilium.testScope string
            Specifies scope of test to be ran (k8s, runtime)
      -cilium.timeout duration
            Specifies timeout for test run (default 24h0m0s)

    Ginkgo ran 1 suite in 4.312100241s
    Test Suite Failed

For more information about other built-in options to Ginkgo, consult the
`ginkgo-documentation`_.

.. _ginkgo-documentation:

Running Specific Tests Within a Test Suite
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

If you want to run one specified test, there are a few options:

* By modifying code: add the prefix "FIt" on the test you want to run; this
  marks the test as focused. Ginkgo will skip other tests and will only run the
  "focused" test. For more information, consult the `Focused Specs`_
  documentation from Ginkgo.

  .. code-block:: go

      It("Example test", func(){
          Expect(true).Should(BeTrue())
      })

      FIt("Example focused test", func(){
          Expect(true).Should(BeTrue())
      })


* From the command line: specify a more granular focus if you want to focus on, say, Runtime L7 tests:

  .. code-block:: shell-session

      INTEGRATION_TESTS=true ginkgo --focus "Runtime.*L7"


This will focus on tests that contain "Runtime", followed by any
number of any characters, followed by "L7". ``--focus`` is a regular
expression and quotes are required if it contains spaces and to escape
shell expansion of ``*``.

.. _Focused Specs: https://onsi.github.io/ginkgo/#focused-specs

Compiling the tests without running them
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

To validate that the Go code you've written for testing is correct without
needing to run the full test, you can build the test directory:

.. code-block:: shell-session

    make -C test/ build

Updating Cilium images for Kubernetes tests
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

Sometimes when running the CI suite for a feature under development, it's common
to re-run the CI suite on the CI VMs running on a local development machine after
applying some changes to Cilium. For this the new Cilium images have to be
built, and then used by the CI suite. To do so, one can run the following
commands on the ``k8s1`` VM:

.. code-block:: shell-session

   cd go/src/github.com/cilium/cilium

   make LOCKDEBUG=1 docker-cilium-image
   docker tag quay.io/cilium/cilium:latest \
	k8s1:5000/cilium/cilium-dev:latest
   docker push k8s1:5000/cilium/cilium-dev:latest

   make -B LOCKDEBUG=1 docker-operator-generic-image
   docker tag quay.io/cilium/operator-generic:latest \
	k8s1:5000/cilium/operator-generic:latest
   docker push k8s1:5000/cilium/operator-generic:latest

The commands were adapted from the ``test/provision/compile.sh`` script.

Test Reports
~~~~~~~~~~~~

The Cilium Ginkgo framework formulates JUnit reports for each test. The
following files currently are generated depending upon the test suite that is ran:

* runtime.xml
* K8s.xml

Best Practices for Writing Tests
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

* Provide informative output to console during a test using the `By construct <https://onsi.github.io/ginkgo/#documenting-complex-specs-by>`_. This helps with debugging and gives those who did not write the test a good idea of what is going on. The lower the barrier of entry is for understanding tests, the better our tests will be!
* Leave the testing environment in the same state that it was in when the test started by deleting resources, resetting configuration, etc.
* Gather logs in the case that a test fails. If a test fails while running on Ginkgo, a postmortem needs to be done to analyze why. So, dumping logs to a location where Ginkgo can pick them up is of the highest imperative. Use the following code in an ``AfterFailed`` method:

.. code-block:: go

	AfterFailed(func() {
		vm.ReportFailed()
	})


Ginkgo Extensions
~~~~~~~~~~~~~~~~~

In Cilium, some Ginkgo features are extended to cover some uses cases that are
useful for testing Cilium.

BeforeAll
^^^^^^^^^

This function will run before all `BeforeEach`_ within a `Describe or Context`_.
This method is an equivalent to ``SetUp`` or initialize functions in common
unit test frameworks.

.. _BeforeEach: https://onsi.github.io/ginkgo/#extracting-common-setup-beforeeach
.. _Describe or Context: https://onsi.github.io/ginkgo/#organizing-specs-with-container-nodes

AfterAll
^^^^^^^^

This method will run after all `AfterEach`_ functions defined in a `Describe or Context`_.
This method is used for tearing down objects created which are used by all
``Its`` within the given ``Context`` or ``Describe``. It is ran after all Its
have ran, this method is a equivalent to ``tearDown`` or ``finalize`` methods in
common unit test frameworks.

A good use case for using ``AfterAll`` method is to remove containers or pods
that are needed for multiple ``Its`` in the given ``Context`` or ``Describe``.

.. _AfterEach: BeforeEach_

JustAfterEach
^^^^^^^^^^^^^

This method will run just after each test and before ``AfterFailed`` and
``AfterEach``. The main reason of this method is to perform some assertions
for a group of tests.  A good example of using a global ``JustAfterEach``
function is for deadlock detection, which checks the Cilium logs for deadlocks
that may have occurred in the duration of the tests.

AfterFailed
^^^^^^^^^^^

This method will run before all ``AfterEach`` and after ``JustAfterEach``. This
function is only called when the test failed.This construct is used to gather
logs, the status of Cilium, etc, which provide data for analysis when tests
fail.

Example Test Layout
^^^^^^^^^^^^^^^^^^^

Here is an example layout of how a test may be written with the aforementioned
constructs:

Test description diagram::

    Describe
        BeforeAll(A)
        AfterAll(A)
        AfterFailed(A)
        AfterEach(A)
        JustAfterEach(A)
        TESTA1
        TESTA2
        TESTA3
        Context
            BeforeAll(B)
            AfterAll(B)
            AfterFailed(B)
            AfterEach(B)
            JustAfterEach(B)
            TESTB1
            TESTB2
            TESTB3


Test execution flow::

    Describe
        BeforeAll
        TESTA1; JustAfterEach(A), AfterFailed(A), AfterEach(A)
        TESTA2; JustAfterEach(A), AfterFailed(A), AfterEach(A)
        TESTA3; JustAfterEach(A), AfterFailed(A), AfterEach(A)
        Context
            BeforeAll(B)
            TESTB1:
               JustAfterEach(B); JustAfterEach(A)
               AfterFailed(B); AfterFailed(A);
               AfterEach(B) ; AfterEach(A);
            TESTB2:
               JustAfterEach(B); JustAfterEach(A)
               AfterFailed(B); AfterFailed(A);
               AfterEach(B) ; AfterEach(A);
            TESTB3:
               JustAfterEach(B); JustAfterEach(A)
               AfterFailed(B); AfterFailed(A);
               AfterEach(B) ; AfterEach(A);
            AfterAll(B)
        AfterAll(A)

Debugging:
~~~~~~~~~~

You can retrieve all run commands and their output in the report directory
(``./test/test_results``). Each test creates a new folder, which contains
a file called log where all information is saved, in case of a failing
test an exhaustive data will be added.

.. code-block:: shell-session

	$ head test/test_results/RuntimeKafkaKafkaPolicyIngress/logs
	level=info msg=Starting testName=RuntimeKafka
	level=info msg="Vagrant: running command \"vagrant ssh-config runtime\""
	cmd: "sudo cilium-dbg status" exitCode: 0
	 KVStore:            Ok         Etcd: 172.17.0.3:4001
	ContainerRuntime:   Ok
	Kubernetes:         Disabled
	Kubernetes APIs:    [""]
	Cilium:             Ok   OK
	NodeMonitor:        Disabled
	Allocated IPv4 addresses:


Running with delve
^^^^^^^^^^^^^^^^^^

`Delve <https://github.com/derekparker/delve>`_ is a debugging tool for Go
applications. If you want to run your test with delve,  you should add a new
breakpoint using
`runtime.BreakPoint() <https://golang.org/pkg/runtime/#Breakpoint>`_ in the
code, and run ginkgo using ``dlv``.

Example how to run ginkgo using ``dlv``:

.. code-block:: shell-session

	dlv test . -- --ginkgo.focus="Runtime" -ginkgo.v=true

Running End-To-End Tests In Other Environments via kubeconfig
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

You can run the end-to-end tests with an arbitrary kubeconfig file by specifying
``--cilium.kubeconfig`` parameter on the Ginkgo command line. This will skip
provisioning the environment and some setup tasks like labeling nodes for testing.

This mode expects:

- The current directory is ``cilium/test``

- A test focus with ``--focus``. ``--focus="K8s"`` selects all kubernetes tests.
  If not passing ``--focus=K8s`` then you must pass ``-cilium.testScope=K8s``.

- Cilium images as full URLs specified with the ``--cilium.image`` and
  ``--cilium.operator-image`` options.

- A working kubeconfig with the ``--cilium.kubeconfig`` option

- A populated K8S_VERSION environment variable set to the version of the cluster

- If appropriate, set the ``CNI_INTEGRATION`` environment variable set to one
  of ``gke``, ``eks``, ``eks-chaining``, ``microk8s`` or ``minikube``. This selects
  matching configuration overrides for cilium.
  Leaving this unset for non-matching integrations is also correct.

  For k8s environments that invoke an authentication agent, such as EKS and
  ``aws-iam-authenticator``, set ``--cilium.passCLIEnvironment=true``

An example invocation is

.. code-block:: shell-session

  INTEGRATION_TESTS=true CNI_INTEGRATION=eks K8S_VERSION=1.16 ginkgo --focus="K8s" -- -cilium.kubeconfig=`echo ~/.kube/config` -cilium.image="quay.io/cilium/cilium-ci" -cilium.operator-image="quay.io/cilium/operator" -cilium.operator-suffix="-ci" -cilium.passCLIEnvironment=true


To run tests with Kind, try

.. code-block:: shell-session

  K8S_VERSION=1.25 ginkgo --focus=K8s -- --cilium.image=localhost:5000/cilium/cilium-dev -cilium.tag=local  --cilium.operator-image=localhost:5000/cilium/operator -cilium.operator-tag=local -cilium.kubeconfig=`echo ~/.kube/config` -cilium.testScope=K8s -cilium.operator-suffix=


Running in GKE
^^^^^^^^^^^^^^

1- Setup a cluster as in :ref:`k8s_install_quick` or utilize an existing
cluster.

.. note:: You do not need to deploy Cilium in this step, as the End-To-End
          Testing Framework handles the deployment of Cilium.

.. note:: The tests require machines larger than ``n1-standard-4``. This can be
          set with ``--machine-type n1-standard-4`` on cluster creation.


2- Invoke the tests from ``cilium/test`` with options set as explained in
`Running End-To-End Tests In Other Environments via kubeconfig`_

.. note:: The tests require the ``NATIVE_CIDR`` environment variable to be set to
          the value of the cluster IPv4 CIDR returned by the ``gcloud container
          clusters describe`` command.

.. code-block:: shell-session

  export CLUSTER_NAME=cluster1
  export CLUSTER_ZONE=us-west2-a
  export NATIVE_CIDR="$(gcloud container clusters describe $CLUSTER_NAME --zone $CLUSTER_ZONE --format 'value(clusterIpv4Cidr)')"

  INTEGRATION_TESTS=true CNI_INTEGRATION=gke K8S_VERSION=1.17 ginkgo --focus="K8sDemo" -- -cilium.kubeconfig=`echo ~/.kube/config` -cilium.image="quay.io/cilium/cilium-ci" -cilium.operator-image="quay.io/cilium/operator" -cilium.operator-suffix="-ci" -cilium.hubble-relay-image="quay.io/cilium/hubble-relay-ci" -cilium.passCLIEnvironment=true

.. note:: The kubernetes version defaults to 1.23 but can be configured with
          versions between 1.16 and 1.23. Version should match the server
          version reported by ``kubectl version``.

AKS (experimental)
^^^^^^^^^^^^^^^^^^

.. note:: The tests require the ``NATIVE_CIDR`` environment variable to be set to
          the value of the cluster IPv4 CIDR.

1. Setup a cluster as in :ref:`k8s_install_quick` or utilize an existing
   cluster. You do not need to deploy Cilium in this step, as the End-To-End
   Testing Framework handles the deployment of Cilium.

2. Invoke the tests from ``cilium/test`` with options set as explained in
`Running End-To-End Tests In Other Environments via kubeconfig`_

.. code-block:: shell-session

    export NATIVE_CIDR="10.241.0.0/16"
    INTEGRATION_TESTS=true CNI_INTEGRATION=aks K8S_VERSION=1.17 ginkgo --focus="K8s" -- -cilium.kubeconfig=`echo ~/.kube/config` -cilium.passCLIEnvironment=true -cilium.image="mcr.microsoft.com/oss/cilium/cilium" -cilium.tag="1.12.1" -cilium.operator-image="mcr.microsoft.com/oss/cilium/operator" -cilium.operator-suffix=""  -cilium.operator-tag="1.12.1"

AWS EKS (experimental)
^^^^^^^^^^^^^^^^^^^^^^

Not all tests can succeed on EKS. Many do, however and may be useful.
:gh-issue:`9678#issuecomment-749350425` contains a list of tests that are still
failing.

1. Setup a cluster as in :ref:`k8s_install_quick` or utilize an existing
   cluster.

2. Source the testing integration script from ``cilium/contrib/testing/integrations.sh``.

3. Invoke the ``gks`` function by passing which ``cilium`` docker image to run
   and the test focus. The command also accepts additional ginkgo arguments.

.. code-block:: shell-session

    gks quay.io/cilium/cilium:latest K8sDemo


Adding new Managed Kubernetes providers
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

All Managed Kubernetes test support relies on using a pre-configured kubeconfig
file.  This isn't always adequate, however, and adding defaults specific to
each provider is possible. The `commit adding GKE <https://github.com/cilium/cilium/commit/c2d8445fd725c515a635c8c3ad3be901a08084eb>`_
support is a good reference.

1. Add a map of helm settings to act as an override for this provider in
   `test/helpers/kubectl.go <https://github.com/cilium/cilium/blob/26dec4c4f4311df2b1a6c909b27ff7fe6e46929f/test/helpers/kubectl.go#L80-L102>`_.
   These should be the helm settings used when generating cilium specs for this
   provider.

2. Add a unique `CI Integration constant <https://github.com/cilium/cilium/blob/26dec4c4f4311df2b1a6c909b27ff7fe6e46929f/test/helpers/kubectl.go#L66-L67>`_.
   This value is passed in when invoking ginkgo via the ``CNI_INTEGRATON``
   environment variable.

3. Update the `helm overrides <https://github.com/cilium/cilium/blob/26dec4c4f4311df2b1a6c909b27ff7fe6e46929f/test/helpers/kubectl.go#L138-L147>`_
   mapping with the constant and the helm settings.

4. For cases where a test should be skipped use the ``SkipIfIntegration``. To
   skip whole contexts, use ``SkipContextIf``. More complex logic can be
   expressed with functions like ``IsIntegration``. These functions are all
   part of the `test/helpers <https://github.com/cilium/cilium/tree/26dec4c4f4311df2b1a6c909b27ff7fe6e46929f/test/helpers>`_
   package.

Running End-To-End Tests In Other Environments via SSH
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

If you want to run tests in an arbitrary environment with SSH access, you can
use ``--cilium.SSHConfig`` to provide the SSH configuration of the endpoint on
which tests will be run. The tests presume the following on the remote
instance:

- Cilium source code is located in the directory ``/home/$USER/go/src/github.com/cilium/cilium/``.
- Cilium is installed and running.

The ssh connection needs to be defined as a ``ssh-config`` file and need to have
the following targets:

- runtime: To run runtime tests
- k8s{1..2}-${K8S_VERSION}: to run Kubernetes tests. These instances must have
  Kubernetes installed and running as a prerequisite for running tests.

An example ``ssh-config`` can be the following:

::

	Host runtime
	  HostName 127.0.0.1
	  User vagrant
	  Port 2222
	  UserKnownHostsFile /dev/null
	  StrictHostKeyChecking no
	  PasswordAuthentication no
	  IdentityFile /home/eloy/.go/src/github.com/cilium/cilium/test/.vagrant/machines/runtime/virtualbox/private_key
	  IdentitiesOnly yes
	  LogLevel FATAL

To run this you can use the following command:

.. code-block:: shell-session

    ginkgo -- --cilium.SSHConfig="cat ssh-config"


Environment variables
~~~~~~~~~~~~~~~~~~~~~

There are a variety of configuration options that can be passed as environment variables:

+----------------------+-------------------+--------------+------------------------------------------------------------------+
| ENV variable         | Default Value     | Options      | Description                                                      |
+======================+===================+==============+==================================================================+
| K8S\_NODES           | 2                 | 0..100       | Number of Kubernetes nodes in the cluster                        |
+----------------------+-------------------+--------------+------------------------------------------------------------------+
| NO_CILIUM_ON_NODE[S] | none              | \*           | Comma-separated list of K8s nodes that should not run Cilium     |
+----------------------+-------------------+--------------+------------------------------------------------------------------+
| K8S\_VERSION         | 1.18              | 1.\*\*       | Kubernetes version to install                                    |
+----------------------+-------------------+--------------+------------------------------------------------------------------+
| KUBEPROXY            | 1                 | 0-1          | If 0 the Kubernetes' kube-proxy won't be installed               |
+----------------------+-------------------+--------------+------------------------------------------------------------------+

Further Assistance
~~~~~~~~~~~~~~~~~~

Have a question about how the tests work or want to chat more about improving the
testing infrastructure for Cilium? Hop on over to the ``#testing`` channel on
`Cilium Slack`_.
