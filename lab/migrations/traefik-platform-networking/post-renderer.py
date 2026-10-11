#!/usr/bin/env python3
"""Read-only candidate renderer: retain Helm ownership; move exact Traefik scope."""
import copy,json,subprocess,sys
SOURCE='platform-system';TARGET='platform-networking'
NAMESPACED={(kind,'traefik') for kind in ['ServiceAccount','Service','Deployment']}
EXPECTED=NAMESPACED|{('ClusterRole','traefik-platform-system'),('ClusterRoleBinding','traefik-platform-system'),('IngressClass','traefik')}
def render(objects):
    result=copy.deepcopy(objects);seen=set()
    for obj in result:
        m=obj.get('metadata',{});key=(obj.get('kind'),m.get('name'))
        if key not in EXPECTED or key in seen:raise ValueError('Unexpected or duplicate object')
        seen.add(key)
        if key in NAMESPACED:
            if m.get('namespace')!=SOURCE:raise ValueError('Source namespace drift')
            m['namespace']=TARGET
        elif m.get('namespace'):raise ValueError('Cluster resource has a namespace')
        if key[0]=='ClusterRoleBinding':
            if obj.get('roleRef')!={'apiGroup':'rbac.authorization.k8s.io','kind':'ClusterRole','name':'traefik-platform-system'}:raise ValueError('Role reference drift')
            if obj.get('subjects')!=[{'kind':'ServiceAccount','name':'traefik','namespace':SOURCE}]:raise ValueError('Binding subject drift')
            obj['subjects'][0]['namespace']=TARGET
    if seen!=EXPECTED:raise ValueError('Missing object')
    return result
if __name__=='__main__':
    try:
        raw=subprocess.run(['yq','-o=json','-I=0','.','-'],input=sys.stdin.read(),capture_output=True,text=True,check=True).stdout
        rows=[json.loads(x) for x in raw.splitlines() if x.strip()]
        sys.stdout.write('\n---\n'.join(json.dumps(x) for x in render(rows))+'\n')
    except (ValueError,KeyError,subprocess.CalledProcessError):sys.exit('Traefik candidate rejected: reviewed scope drifted')
