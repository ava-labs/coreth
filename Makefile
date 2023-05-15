# Copyright (C) 2022, Chain4Travel AG. All rights reserved.
#
# This file is a derived work, based on ava-labs code whose
# original notices appear below.
#
# It is distributed under the same license conditions as the
# original code from which it is derived.
#
# Much love to the original authors for their work.
# **********************************************************
# (c) 2020, Ava Labs, Inc. All rights reserved.
# See the file LICENSE for licensing terms.

# Basic Makefile for running Python scripts in the caminoethvm project

.PHONY: python

python:
	python3 python/tests/camino_web3_sdk_test.py
