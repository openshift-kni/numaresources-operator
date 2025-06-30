#!/usr/bin/env python

import argparse
import os
import subprocess
import sys
import yaml


def make_gci_commandline(tree, ciconfig=".golangci.yml"):
    with open(ciconfig) as src:
        cfg = yaml.safe_load(src)
    try:
        sections = cfg['linters-settings']['gci']['sections']
    except KeyError:
        sys.stderr.write("cannot find gci configuration on %s\n" % ciconfig)
        sys.exit(2)
    cmdline = [
        "gci", "write", "--custom-order",
    ]
    for sect in sections:
        cmdline.extend(("-s", sect))
    cmdline.append(tree)
    return cmdline


def _main(args):
    cmdline = make_gci_commandline(args.tree)
    if args.verbose and not args.dry_run:
        sys.stderr.write("running: [%s]\n" % " ".join(cmdline))
    if args.dry_run:
        print(" ".join(cmdline))
        return
    subprocess.run(cmdline)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        prog='sort-imports.py',
        description='sort golang imports according to project standards delegating to gci')
    parser.add_argument('tree')
    parser.add_argument('-v', '--verbose', action='store_true')
    parser.add_argument('-D', '--dry-run', action='store_true')
    args = parser.parse_args()
    _main(args)
