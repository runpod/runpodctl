# Changelog

## [1.14.1](https://github.com/runpod/runpodctl/compare/v1.14.0...v1.14.1) (2024-02-22)


### Bug Fixes

* quotes ([7c260aa](https://github.com/runpod/runpodctl/commit/7c260aa178006cf51bc18d4d8ce0dbabd4f5138f))

## [1.14.0](https://github.com/runpod/runpodctl/compare/v1.13.0...v1.14.0) (2024-02-22)


### Features

* add installer of deps ([97699f9](https://github.com/runpod/runpodctl/commit/97699f917744daac87a91f6c3ad81fe0cf44183b))


### Bug Fixes

* correct verbose for rsync, and dep fix ([8e4d10b](https://github.com/runpod/runpodctl/commit/8e4d10bacf15f0bd069cc44890485024f9d65aa0))
* ignore more rysnc and remove jq ([3309865](https://github.com/runpod/runpodctl/commit/3309865d9176827157e270ea7f12bbf8e6fe1dbf))
* tab to spaces ([cdce259](https://github.com/runpod/runpodctl/commit/cdce259e1baf8a9f57037ef18565de278bbd9694))
* trigger release-please ([5061c14](https://github.com/runpod/runpodctl/commit/5061c1465b4675077a09cf0fe490b4178721ab6f))

## [1.13.0](https://github.com/runpod/runpodctl/compare/v1.12.3...v1.13.0) (2024-02-13)


### Features

* added comments to toml ([d0f1bfa](https://github.com/runpod/runpodctl/commit/d0f1bfa65ac8ac477f429b18b6d19467b3dc3a6a))
* added more starter examples ([57474e5](https://github.com/runpod/runpodctl/commit/57474e592d23b1e1efcf07355081916f480b1574))
* port ssh cmd from python, refactored ssh key gen ([4c90f4b](https://github.com/runpod/runpodctl/commit/4c90f4b740b447b1702193b08efc04ba4decbf26))
* remote exec ([53ce856](https://github.com/runpod/runpodctl/commit/53ce8566be506b4970b7c791bf6cca9e0fd98676))


### Bug Fixes

* added detail errors back ([0de3690](https://github.com/runpod/runpodctl/commit/0de3690ca269f26d4a18ac43b4ee40409ea4f27b))
* expose better printing ([4767fe2](https://github.com/runpod/runpodctl/commit/4767fe2ed1dfd3a2643c4fda4f5d0af92097dc23))
* improved bash resiliency ([3e0eadb](https://github.com/runpod/runpodctl/commit/3e0eadbf0d7594d3a4d18008297f76583b289dde))
* moved doc to docs ([73b6290](https://github.com/runpod/runpodctl/commit/73b629087553180e3a3fb9d02200c2ed8f66df48))
* naming and prompting ([232f373](https://github.com/runpod/runpodctl/commit/232f373d1d56e98f5c8cff6bedbcc8f42ff0b1e1))
* removed unused relay list ([f5cde85](https://github.com/runpod/runpodctl/commit/f5cde856269d71c7c0f14cc02e722a4c451a8503))
* stable diffusion name ([ea2a35a](https://github.com/runpod/runpodctl/commit/ea2a35af5254f5630d948cdfc07da8779a83bf11))
* support --version flag ([4e7ae63](https://github.com/runpod/runpodctl/commit/4e7ae630de784439003f8e45ea846f76431afc30))
* trigger release-please ([b82fd9c](https://github.com/runpod/runpodctl/commit/b82fd9cc2dfa4821946a397d27db0c5f8c9c97fe))

## [1.12.3](https://github.com/runpod/runpodctl/compare/v1.12.2...v1.12.3) (2024-02-06)


### Bug Fixes

* seems the new architectures aren't building in github action ([0cefd3c](https://github.com/runpod/runpodctl/commit/0cefd3c380bbef42cb471f43b37dc224051b9140))

## [1.12.2](https://github.com/runpod/runpodctl/compare/v1.12.1...v1.12.2) (2024-02-06)


### Bug Fixes

* mislabeled in release ([9c850cb](https://github.com/runpod/runpodctl/commit/9c850cbe508cf988dcfde277e8f279dbf246ad25))

## [1.12.1](https://github.com/runpod/runpodctl/compare/v1.12.0...v1.12.1) (2024-02-06)


### Bug Fixes

* fix: the feat: and feat: the fix: ([8f418f3](https://github.com/runpod/runpodctl/commit/8f418f330f07a3ec42490fcf2275734a02d76065))

## [1.12.0](https://github.com/runpod/runpodctl/compare/v1.11.0...v1.12.0) (2024-02-03)


### Features

* add user agent and fix project example ([ef38812](https://github.com/runpod/runpodctl/commit/ef388126577c5983d73c9b67837d9a72df353ea1))
* prompt cleanup and new starter examples ([896a022](https://github.com/runpod/runpodctl/commit/896a02297a68d350ce815d39a399a9e1fe7b8650))


### Bug Fixes

* add -qO- ([15892c2](https://github.com/runpod/runpodctl/commit/15892c2f92299595216797b3d9b8c857ead1fdcc))
* enhance user clarity ([5ac4059](https://github.com/runpod/runpodctl/commit/5ac4059d0f3560d80572defa9ab09e139b8977c9))
* forgot to make a change ([0335586](https://github.com/runpod/runpodctl/commit/0335586b9c44818d58bcba00c1d2653d90649dca))
* rename new to create ([7a7aba0](https://github.com/runpod/runpodctl/commit/7a7aba0df85fba0b40028e4c8b39f43be25ae9da))

## [1.11.0](https://github.com/runpod/runpodctl/compare/v1.10.0...v1.11.0) (2024-01-28)


### Features

* add installer script ([b2a75a7](https://github.com/runpod/runpodctl/commit/b2a75a7f5d16271d898e0d4457500e68fe380b8a))
* cache venvs in tar format ([a0fec98](https://github.com/runpod/runpodctl/commit/a0fec9823d068d063e3f1f5ff77bc4c1815a7b7e))
* flag to print pod logs without prefix ([0f93a1a](https://github.com/runpod/runpodctl/commit/0f93a1a70fcf7742c4e0351c5822c90d177c8014))
* handle deleted network volume, dropdown for starter templates, template Dockerfile ([d042edb](https://github.com/runpod/runpodctl/commit/d042edba17b47a565ea38b17f88bcfe90a98c47f))
* runpodctl project build to emit dockerfile ([7844b5d](https://github.com/runpod/runpodctl/commit/7844b5d2d06aa538c4af0f809fd76ac89554fe76))
* updated readme and requirements ([b800f1f](https://github.com/runpod/runpodctl/commit/b800f1f2a82532577819e83ecaca3ed1994700b4))


### Bug Fixes

* adapt deploy to venv path ([3e9d115](https://github.com/runpod/runpodctl/commit/3e9d115ba027a1b9007759160cdbb333c970b3a9))
* brew install ([01f0c89](https://github.com/runpod/runpodctl/commit/01f0c89d824ffea179fdc1a0f924b09f6edc897b))
* cleanup install script ([c8a76d7](https://github.com/runpod/runpodctl/commit/c8a76d78cdb193c267ce443158ab9d608857230c))
* complete renaming ([3da74ab](https://github.com/runpod/runpodctl/commit/3da74aba882d06adbf49f59da4a2d92d8e327998))
* detect OS earlier ([d9fc83b](https://github.com/runpod/runpodctl/commit/d9fc83bce91dec8d26e9e6eca319921df6acdb3d))
* hack to make runpodignore use gitignore syntax on the server ([9e1dd71](https://github.com/runpod/runpodctl/commit/9e1dd71aba05a6ffddd43d74c6a582d5fa760448))
* improve install script ([eeed522](https://github.com/runpod/runpodctl/commit/eeed522c066998ab510460bef9f8cd8161d91de7))
* persistent typo ([02f38db](https://github.com/runpod/runpodctl/commit/02f38dbc9e11d2f22028538f73f67c938738be61))
* readme rename ([d8ed9c7](https://github.com/runpod/runpodctl/commit/d8ed9c74621ee092b99900ab076fed11f58b0b92))
* rename runpodctl to runpod ([223dd40](https://github.com/runpod/runpodctl/commit/223dd40102dcf8a460049d04e90ff334471ca3c6))
* update dockerfile ([138bd15](https://github.com/runpod/runpodctl/commit/138bd15551bd3d3ecb0ace33900f926b74f89603))
* update root name ([270172d](https://github.com/runpod/runpodctl/commit/270172d24f1083c88ccbedf3cd22c8bef4d4c32d))
* version check ([0536a8a](https://github.com/runpod/runpodctl/commit/0536a8a4adb687ab927be64e695e1557985a6116))
* version check order ([066a07c](https://github.com/runpod/runpodctl/commit/066a07c55c013a134d19c2b7f3d0962fbb37bb5d))
* windows antivirus doesn't let us do the right update logic ([85e7bbb](https://github.com/runpod/runpodctl/commit/85e7bbb83b77b78b70d408444c42f8901036ba6d))
* windows compatibility and toml fix ([004a306](https://github.com/runpod/runpodctl/commit/004a30610726f67641b412f1f209848bbb3e0f45))

## [1.10.0](https://github.com/runpod/runpodctl/compare/v1.9.0...v1.10.0) (2023-04-08)


### Features

* add templateId to create pod ([073fd04](https://github.com/runpod/runpodctl/commit/073fd04052f05df3db0312c000d5d70aac8ca0ec))
* update docs ([05be3be](https://github.com/runpod/runpodctl/commit/05be3be33826f3840adf7f315c502865480fa91d))


### Bug Fixes

* Updated README to include mac (amd) intel installation steps ([82b91d6](https://github.com/runpod/runpodctl/commit/82b91d6ebd4b2fea214155346134d84161387b30))

## [1.9.0](https://github.com/runpod/runpodctl/compare/v1.8.0...v1.9.0) (2023-02-09)


### Features

* allow multiple gpu type ids using comma ([79918c6](https://github.com/runpod/runpodctl/commit/79918c6ca973a05c20acdc4e5ff622d0566f0fe9))
* allow multiple pods to be created / removed ([fd88b4f](https://github.com/runpod/runpodctl/commit/fd88b4f39f6c4e700226f09f12b8e9db512f303e))


### Bug Fixes

* lint ([53baa35](https://github.com/runpod/runpodctl/commit/53baa35e1deee68f409c4fcbad296c2553649655))

## [1.8.0](https://github.com/runpod/runpodctl/compare/v1.7.0...v1.8.0) (2023-01-09)


### Features

* encode relay index into secret ([2b3a0a3](https://github.com/runpod/runpodctl/commit/2b3a0a326760606121dd6746e1dbc62dcdeb0c79))
* support multiple relays ([b14ca04](https://github.com/runpod/runpodctl/commit/b14ca04737efad73312b958816a735ead3137787))
* use - instead of :: ([e587638](https://github.com/runpod/runpodctl/commit/e587638a75cdb02b280944e5d15f6f6aaf36d379))
* use file in main branch for relays ([4ee50fc](https://github.com/runpod/runpodctl/commit/4ee50fcc403230d2f7789802ed4408660be591c4))
* use main file ([710a883](https://github.com/runpod/runpodctl/commit/710a8830d51ca8b343600009dea7538ae7ae6903))

## [1.7.0](https://github.com/runpod/runpodctl/compare/v1.6.1...v1.7.0) (2022-12-10)


### Features

* add more thanks to readme ([9200b06](https://github.com/runpod/runpodctl/commit/9200b0613d0ccfd2ad94f34ee07ef0cdb3a132bd))
* omit deployCost if 0, and populate pod name if empty ([76c420f](https://github.com/runpod/runpodctl/commit/76c420f34c633e5c0edee0fa7f15157d82aa891a))
* update readme with how to transfer data ([a07026f](https://github.com/runpod/runpodctl/commit/a07026f53171febc5754fb93a977e84b06320f36))

## [1.6.1](https://github.com/Run-Pod/runpodctl/compare/v1.6.0...v1.6.1) (2022-10-04)


### Bug Fixes

* add modules ([99af571](https://github.com/Run-Pod/runpodctl/commit/99af571227a41e4eab106e78af7e5097910ef8db))

## [1.6.0](https://github.com/Run-Pod/runpodctl/compare/v1.5.0...v1.6.0) (2022-10-04)


### Features

* use croc lib to add send and receive ([be3c162](https://github.com/Run-Pod/runpodctl/commit/be3c1620ff5c37060714e026fd810d1069e985ac))

## [1.5.0](https://github.com/Run-Pod/runpodctl/compare/v1.4.0...v1.5.0) (2022-08-17)


### Features

* allow RUNPOD_API_URL env if api url is not defined in config ([6e012f6](https://github.com/Run-Pod/runpodctl/commit/6e012f6f55680a72192c144192a10556d9cc497a))

## [1.4.0](https://github.com/Run-Pod/runpodctl/compare/v1.3.0...v1.4.0) (2022-07-21)


### Features

* allow RUNPOD_API_KEY env to overwrite config api key ([e38f6a3](https://github.com/Run-Pod/runpodctl/commit/e38f6a3ead2b5dbaa5ef4d4d7014ee925a07b128))

## [1.3.0](https://github.com/Run-Pod/runpodctl/compare/v1.2.0...v1.3.0) (2022-04-12)


### Features

* add windows build ([7e1d614](https://github.com/Run-Pod/runpodctl/commit/7e1d614e478bfe3bf92ff1a85a806d7961b8e129))
* create pod ([fddf882](https://github.com/Run-Pod/runpodctl/commit/fddf882211f30e6f5822f59e2cdc2ebb3077c24f))
* implement get cloud ([bf4cad6](https://github.com/Run-Pod/runpodctl/commit/bf4cad661ca0a14d3a13afbe6b3263a749e4efd2))
* update readme ([11ea5b6](https://github.com/Run-Pod/runpodctl/commit/11ea5b6af9863fbf0b89a209f92323b932eec3d3))

## [1.2.0](https://github.com/Run-Pod/runpodctl/compare/v1.1.0...v1.2.0) (2022-04-12)


### Features

* add ondemand pod start ([2b8ddfa](https://github.com/Run-Pod/runpodctl/commit/2b8ddfaf87436785b12cbc03541d43f8e499b6b1))
* update docs ([00ef4d3](https://github.com/Run-Pod/runpodctl/commit/00ef4d377adba4fa69667a081a70869c3c7fcbfe))


### Bug Fixes

* api key argument ([ad750c8](https://github.com/Run-Pod/runpodctl/commit/ad750c8079e447158729e6ca0718a835608039e7))
* release link ([5929dc7](https://github.com/Run-Pod/runpodctl/commit/5929dc74ea6fefcc7560c4b95fb0e5ae74b34f22))
* release link ([6180027](https://github.com/Run-Pod/runpodctl/commit/61800275ab24f40bcadf909a1386ebad01032544))

## [1.1.0](https://github.com/Run-Pod/runpodctl/compare/v1.0.0...v1.1.0) (2022-04-11)


### Features

* add darwin arm build ([9273929](https://github.com/Run-Pod/runpodctl/commit/92739299871660cd977681a782bca6d52c586a0e))
* add to readme ([0986e69](https://github.com/Run-Pod/runpodctl/commit/0986e693ebc4d936b289b7b6e806f9fe500a5a27))
* add version command ([bce6eeb](https://github.com/Run-Pod/runpodctl/commit/bce6eeb4e02737603f2aae3627c81ea1266459a9))


### Bug Fixes

* config api url ([aa8a6e6](https://github.com/Run-Pod/runpodctl/commit/aa8a6e603873df94d6930f4ec9155ed24273de24))

## 1.0.0 (2022-04-11)


### Features

* add docs ([8d2ff09](https://github.com/Run-Pod/runpodctl/commit/8d2ff09e66eaa110b281a0c47711333f5ae18fe9))
* add docs ([ea26e30](https://github.com/Run-Pod/runpodctl/commit/ea26e30939e165b7805e21321b16c0a96247d7cb))
* add git action for release ([fb536d0](https://github.com/Run-Pod/runpodctl/commit/fb536d0e9392f04a6dfd362e928be3d24a59ab4f))
* implement remove a pod ([59851d9](https://github.com/Run-Pod/runpodctl/commit/59851d950a39446610e97fa910466cab468eefc1))
* implement start pod with bid price ([707dcb4](https://github.com/Run-Pod/runpodctl/commit/707dcb45efdec9294113186648734ec24e9d39f9))
* implement stop pod ([d1e3aed](https://github.com/Run-Pod/runpodctl/commit/d1e3aed25513245cffa6a6579f815e2bfb7733b6))
* make apiUrl configurable ([d7fd073](https://github.com/Run-Pod/runpodctl/commit/d7fd0733c32f405d750d984c43396369ae4c6a32))
* move table to format folder ([5531a5e](https://github.com/Run-Pod/runpodctl/commit/5531a5e99b8fb8d520966fc5277751f0870dabdf))
* use cobra, implement config and pod ls ([d6bf47f](https://github.com/Run-Pod/runpodctl/commit/d6bf47f683e90873a160c873c74523a3023a4ac0))
* use get resource format to define commands ([1f187a1](https://github.com/Run-Pod/runpodctl/commit/1f187a195195c43549c47d9bc0e2be90c812d771))
