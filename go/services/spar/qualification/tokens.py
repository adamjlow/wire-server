"""Run selected unchanged published originals against Go Spar and Haskell peers."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

sys.path.insert(0,'/opt/wire-e2e')
import harness
import runner
import runtime

TOKEN_TESTS=['Spar.testSparCreateScimTokenNoName','Spar.testSparCreateScimTokenWithName']
IDP_TESTS=TOKEN_TESTS+['Spar.testSparCreateScimTokenAssocImplicitly','Spar.testSparCreateTwoScimTokensForOneIdp','Spar.testCheckAdminGetTeamId','Spar.testCheckAdminGetTeamIdV7']
LOGIN_TESTS=['Spar.testSparEmulateSPInitiatedLogin','Spar.testSparSPInitiatedLoginWithUtf8']
SCIM_GROUP_TESTS=['Spar.testSparScimCreateGetSearchUserGroup','Spar.testSparScimUpdateUserGroup','Spar.testSparScimUpdateUserGroupRejectsInvalidMembers','Spar.testSparScimDeleteUserGroup','Spar.testSparScimGroupSearchOnlyReturnsScimGroups']
SCIM_USER_TESTS=['Spar.'+name for name in ['testSparExternalIdDifferentFromEmail','testSparExternalIdDifferentFromEmailWithIdp','testSparExternalIdUpdateToANonEmail','testSparPatchEmailValuePath','testSparPatchEmailValuePathInPlace','testSparPatchEmailSubAttrRemoveNull','testSparRejectsMultiplePrimaryEmails','testSparScimEmailMetaRoundTrip','testSparScimEmailTypeNonWorkEcho','testSparScimInvalidatesPendingEmail','testScimUpdateEmailAddressAndExternalId']]+['Spar.testSparMigrateFromExternalIdOnlyToEmail[mailunchanged='+v+']' for v in ('false','true')]+['Spar.testScimUpdateEmailAddress[extidisemail='+ext+'][requireexternalemailverification='+verify+']' for ext in ('false','true') for verify in ('false','true')]

def main():
    parser=argparse.ArgumentParser();parser.add_argument('--suite',choices=('tokens','idps','delete','login','scim-groups','scim-users'),default='tokens');args=parser.parse_args()
    tests={'tokens':TOKEN_TESTS,'idps':IDP_TESTS,'delete':['Spar.IdPIssuerUpdate.testUpdateIdPIssuerThenDelete'],'login':LOGIN_TESTS,'scim-groups':SCIM_GROUP_TESTS,'scim-users':SCIM_USER_TESTS}[args.suite]
    if os.getuid()!=65534 or os.environ.get('WIRE_E2E_DISPOSABLE')!='1':raise ValueError('owned nonroot disposable environment required')
    output={'kind':'original-Go-Spar-token-tests' if args.suite=='tokens' else 'original-Go-Spar-'+args.suite+'-tests','result':'running','e2e_executed':False,'required_tests':tests,'scope':'Selected original construction tests only; full Spar and seven-Go qualification incomplete.'}
    processes=[];logs=[];servers=[]
    try:
        if not set(tests).issubset(runner.discover('spar')):raise ValueError('original required names absent')
        servers=runtime.start_forwards();runtime.wait_dependencies();config=runtime.stage();runtime.initialize()
        candidate=Path('/candidate/spar').resolve(strict=True)
        link=runtime.WORK/'dist/spar';link.unlink();link.symlink_to(candidate)
        output['implementations']={link.name:{'implementation':'go' if link.name=='spar' else 'haskell','path':str(link.resolve(strict=True)),'sha256':hashlib.sha256(link.read_bytes()).hexdigest()} for link in (runtime.WORK/'dist').iterdir() if link.name not in ('integration','nginx')}
        runtime.start_peers(processes,logs);os.chdir(runtime.WORK)
        xml=Path('/reports/tokens.xml')
        binary=runner.binary('integration');output['integration']=binary
        result=subprocess.run([binary['path'],'--config',str(config),*[arg for name in tests for arg in ('--include',name)],'--xml',str(xml)],timeout=300)
        output.update(e2e_executed=True,process_exit_code=result.returncode)
        if result.returncode!=0 or not xml.is_file():raise ValueError('selected original execution failed')
        output['validated_report']=harness.check_xml(xml,tests);output['result']='pass';return 0
    except Exception as error:
        output.update(result='fail',error_category=type(error).__name__,error=str(error)[:300]);return 1
    finally:
        for process in processes:
            if process.poll()is None:process.terminate()
        for process in processes:
            try:process.wait(timeout=5)
            except subprocess.TimeoutExpired:process.kill();process.wait(timeout=5)
        for log in logs:log.close()
        for server in servers:server.server_close()
        Path('/reports/result.json').write_text(json.dumps(output,indent=2)+'\n')

if __name__=='__main__':raise SystemExit(main())
