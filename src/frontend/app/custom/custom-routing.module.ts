import { NgModule } from '@angular/core';
import { Routes, RouterModule } from '@angular/router';

const kubernetes: Routes = [{
  path: 'kubernetes',
  loadChildren: 'app/custom/kubernetes/kubernetes.module#KubernetesModule',
  data: {
    stratosNavigation: {
      text: 'Kubernetes',
      matIcon: 'kubernetes',
      matIconFont: 'stratos-icons',
      position: 60,
      requiresEndpointType: 'k8s'
    }
  }
}];

@NgModule({
  imports: [
    RouterModule.forRoot(kubernetes),
  ],
  declarations: []
})
export class CustomRoutingModule { }
