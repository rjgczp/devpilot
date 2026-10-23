"""Inventory all compiled Budget tests; specialized physical suites run separately."""
import hashlib,json,subprocess,sys
from pathlib import Path
repo=Path('/Users/sansea/Documents/ChatGPT/devpilot');tools=Path('/private/tmp/devpilot-joint63-denial-tools');out=Path('/private/tmp/devpilot-joint63-denial-budget');out.mkdir(exist_ok=False)
original=Path('/private/tmp/devpilot-release54-delivery-e3a3d03/budget-regression-runner.py')
source=original.read_text();old='/private/tmp/devpilot-release54-budget-tools';new=str(tools)
assert source.count(old)==2
adapted=out/'guarded-runner.py';adapted.write_text(source.replace(old,new))
(out/'runner-provenance.json').write_text(json.dumps(dict(original=str(original),original_sha256=hashlib.sha256(original.read_bytes()).hexdigest(),only_path_substitution=dict(old=old,new=new),adapted_sha256=hashlib.sha256(adapted.read_bytes()).hexdigest()),indent=2)+'\n')
special={'TestBudgetNetworkPolicyPG':'network64','TestBudgetPhysicalStopPG':'stop65','TestBudgetPhysicalReleasePG':'release54','TestBudgetJointKernelPG':'joint46'}
for mode,name in [('normal','joint-pg.test'),('race','joint-pg-race.test')]:
 cmd=['docker','run','--rm','--network','none','--cap-drop','ALL','--security-opt','no-new-privileges:true','-v',str(tools)+':/toolbin:ro','--entrypoint','/toolbin/'+name,'devpilot-scheduler-fixture','-test.list','^TestBudget']
 names=subprocess.check_output(cmd,text=True).splitlines();assert set(special)<=set(names)
 pure=[n for n in names if n not in special];assert pure and len(names)==len(set(names))
 (out/(mode+'-plan.json')).write_text(json.dumps(dict(compiled=names,pure_database=pure,specialized_already_executed=special,listing_command=cmd),indent=2)+'\n')
 expression='^('+'|'.join(pure)+')$'
 call=[sys.executable,'-B',str(adapted),'--binary',str(tools/name),'--evidence',str(out/mode),'--name',mode,'--run',expression]
 code=subprocess.run([sys.executable,str(repo/'tests/scheduler/record.py'),'--evidence',str(out/'receipts'),'--name',mode,'--',*call],cwd=repo).returncode
 if code:raise SystemExit(code)
(out/'result.json').write_text(json.dumps(dict(complete=True,all_compiled_budget_tests_accounted=True),indent=2)+'\n')
